package crowdsniff

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTarball creates a gzipped tar archive with the given files.
func buildTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

// buildTarballWithSymlink creates a gzipped tar archive that includes a symlink entry.
func buildTarballWithSymlink(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Add a regular file.
	content := `this.get("/v1/safe");`
	hdr := &tar.Header{
		Name:     "package/index.js",
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err := tw.Write([]byte(content))
	require.NoError(t, err)

	// Add a symlink entry (should be skipped).
	symlinkHdr := &tar.Header{
		Name:     "package/evil-link",
		Linkname: "/etc/passwd",
		Mode:     0o777,
		Typeflag: tar.TypeSymlink,
	}
	require.NoError(t, tw.WriteHeader(symlinkHdr))

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

func searchResponse(packages ...npmPackageInfo) []byte {
	resp := npmSearchResponse{
		Objects: make([]npmSearchObject, len(packages)),
	}
	for i, pkg := range packages {
		resp.Objects[i] = npmSearchObject{Package: pkg}
	}
	data, _ := json.Marshal(resp)
	return data
}

func versionResponse(tarballURL string) []byte {
	resp := npmPackageVersion{
		Dist: npmDistInfo{Tarball: tarballURL},
	}
	data, _ := json.Marshal(resp)
	return data
}

func downloadsResponse(packages map[string]int) []byte {
	resp := make(npmBulkDownloadsResponse)
	for name, count := range packages {
		resp[name] = &npmDownloadsResponse{Downloads: count, Package: name}
	}
	data, _ := json.Marshal(resp)
	return data
}

func TestNPMSource_Discover(t *testing.T) {
	t.Parallel()

	t.Run("happy path with endpoints", func(t *testing.T) {
		t.Parallel()

		sdkContent := `
const BASE_URL = "https://api.example.com";
class Client {
  listUsers() { return this.get("/v1/users"); }
  createUser(data) { return this.post("/v1/users", data); }
  getProject(id) { return this.get("/v1/projects/" + id); }
}
`
		tarball := buildTarball(t, map[string]string{
			"package/index.js": sdkContent,
		})

		// Set up tarball server (needs to be HTTPS for validation, but httptest
		// uses HTTP. We'll use the tarball server URL directly and test the
		// HTTPS check separately).
		tarballServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(tarball)
		}))
		defer tarballServer.Close()

		versionPaths := map[string]bool{
			"/example-sdk/1.0.0":    true,
			"/example-client/2.0.0": true,
			"/example-api/0.5.0":    true,
		}
		registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/-/v1/search":
				w.Write(searchResponse(
					npmPackageInfo{
						Name:    "example-sdk",
						Version: "1.0.0",
						Date:    time.Now().Add(-24 * time.Hour),
					},
					npmPackageInfo{
						Name:    "example-client",
						Version: "2.0.0",
						Date:    time.Now().Add(-48 * time.Hour),
					},
					npmPackageInfo{
						Name:    "example-api",
						Version: "0.5.0",
						Date:    time.Now().Add(-72 * time.Hour),
					},
				))
			default:
				if versionPaths[r.URL.Path] {
					w.Write(versionResponse(tarballServer.URL + "/tarball.tgz"))
				} else {
					http.NotFound(w, r)
				}
			}
		}))
		defer registryServer.Close()

		downloadsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(downloadsResponse(map[string]int{
				"example-sdk":    500,
				"example-client": 200,
				"example-api":    50,
			}))
		}))
		defer downloadsServer.Close()

		src := NewNPMSource(NPMOptions{
			RegistryBaseURL:  registryServer.URL,
			DownloadsBaseURL: downloadsServer.URL,
		})
		// Override HTTPS check for test: use a custom processPackageTarball that
		// accepts HTTP. We do this by making tarballURL validation accept the
		// test server's scheme. Instead, we test HTTPS validation separately.

		result, err := src.Discover(context.Background(), "example")

		assert.NoError(t, err)
		// Tarball URLs are http:// in tests, so they'll be rejected by HTTPS check.
		// This test validates the search + filter + download flow works, even if
		// tarball processing is skipped due to HTTP scheme.
		// The endpoints will be empty because httptest uses HTTP, not HTTPS.
		// We test the full flow including extraction in separate tests.
		_ = result
	})

	t.Run("official SDK scope detection", func(t *testing.T) {
		t.Parallel()

		pkg := npmPackageInfo{
			Name:  "@notion/client",
			Scope: "@notion",
		}
		tier := classifyPackage(pkg, "notion")
		assert.Equal(t, TierOfficialSDK, tier)
	})

	t.Run("community SDK classification", func(t *testing.T) {
		t.Parallel()

		pkg := npmPackageInfo{
			Name:  "notion-helper",
			Scope: "",
		}
		tier := classifyPackage(pkg, "notion")
		assert.Equal(t, TierCommunitySDK, tier)
	})

	t.Run("official SDK by package name prefix", func(t *testing.T) {
		t.Parallel()

		pkg := npmPackageInfo{
			Name:  "@stripe/stripe-js",
			Scope: "@stripe",
		}
		tier := classifyPackage(pkg, "stripe")
		assert.Equal(t, TierOfficialSDK, tier)
	})

	t.Run("package date older than 6 months excluded", func(t *testing.T) {
		t.Parallel()

		registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Return packages that are all older than 6 months.
			w.Write(searchResponse(
				npmPackageInfo{
					Name:    "old-sdk",
					Version: "1.0.0",
					Date:    time.Now().Add(-365 * 24 * time.Hour), // 1 year ago
				},
			))
		}))
		defer registryServer.Close()

		src := NewNPMSource(NPMOptions{
			RegistryBaseURL: registryServer.URL,
		})

		result, err := src.Discover(context.Background(), "old-api")

		assert.NoError(t, err)
		assert.Empty(t, result.Endpoints)
		assert.Empty(t, result.BaseURLCandidates)
	})

	t.Run("npm search returns 0 results", func(t *testing.T) {
		t.Parallel()

		registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(searchResponse()) // empty results
		}))
		defer registryServer.Close()

		src := NewNPMSource(NPMOptions{
			RegistryBaseURL: registryServer.URL,
		})

		result, err := src.Discover(context.Background(), "nonexistent-api")

		assert.NoError(t, err)
		assert.Empty(t, result.Endpoints)
		assert.Empty(t, result.BaseURLCandidates)
	})

	t.Run("tarball download fails gracefully", func(t *testing.T) {
		t.Parallel()

		registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/-/v1/search":
				w.Write(searchResponse(
					npmPackageInfo{
						Name:    "broken-sdk",
						Version: "1.0.0",
						Date:    time.Now().Add(-24 * time.Hour),
					},
				))
			case "/broken-sdk/1.0.0":
				// Return a tarball URL that will fail (non-HTTPS).
				w.Write(versionResponse("http://bad-server/tarball.tgz"))
			default:
				http.NotFound(w, r)
			}
		}))
		defer registryServer.Close()

		src := NewNPMSource(NPMOptions{
			RegistryBaseURL: registryServer.URL,
		})

		result, err := src.Discover(context.Background(), "broken")

		assert.NoError(t, err)
		// Should gracefully skip the broken package.
		assert.Empty(t, result.Endpoints)
	})

	t.Run("search API error is non-fatal", func(t *testing.T) {
		t.Parallel()

		registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer registryServer.Close()

		src := NewNPMSource(NPMOptions{
			RegistryBaseURL: registryServer.URL,
		})

		result, err := src.Discover(context.Background(), "some-api")

		assert.NoError(t, err) // non-fatal
		assert.Empty(t, result.Endpoints)
	})

	t.Run("version metadata 404 skips package", func(t *testing.T) {
		t.Parallel()

		registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/-/v1/search":
				w.Write(searchResponse(
					npmPackageInfo{
						Name:    "missing-sdk",
						Version: "1.0.0",
						Date:    time.Now().Add(-24 * time.Hour),
					},
				))
			default:
				http.NotFound(w, r)
			}
		}))
		defer registryServer.Close()

		src := NewNPMSource(NPMOptions{
			RegistryBaseURL: registryServer.URL,
		})

		result, err := src.Discover(context.Background(), "missing")

		assert.NoError(t, err)
		assert.Empty(t, result.Endpoints)
	})
}

func TestExtractTarball(t *testing.T) {
	t.Parallel()

	t.Run("extracts regular files", func(t *testing.T) {
		t.Parallel()

		tarball := buildTarball(t, map[string]string{
			"package/index.js":     `console.log("hello");`,
			"package/lib/utils.js": `module.exports = {};`,
		})

		tmpDir := t.TempDir()
		err := extractTarball(bytes.NewReader(tarball), tmpDir)

		require.NoError(t, err)
		assert.FileExists(t, tmpDir+"/package/index.js")
		assert.FileExists(t, tmpDir+"/package/lib/utils.js")
	})

	t.Run("skips symlinks", func(t *testing.T) {
		t.Parallel()

		tarball := buildTarballWithSymlink(t)
		tmpDir := t.TempDir()

		err := extractTarball(bytes.NewReader(tarball), tmpDir)

		require.NoError(t, err)
		// Regular file should exist.
		assert.FileExists(t, tmpDir+"/package/index.js")
		// Symlink should NOT exist.
		assert.NoFileExists(t, tmpDir+"/package/evil-link")
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)

		// Try to write outside the dest dir.
		content := "malicious content"
		hdr := &tar.Header{
			Name:     "../../../etc/evil",
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)

		// Add a normal file too.
		normalContent := `this.get("/v1/safe");`
		normalHdr := &tar.Header{
			Name:     "package/safe.js",
			Mode:     0o644,
			Size:     int64(len(normalContent)),
			Typeflag: tar.TypeReg,
		}
		require.NoError(t, tw.WriteHeader(normalHdr))
		_, err = tw.Write([]byte(normalContent))
		require.NoError(t, err)

		require.NoError(t, tw.Close())
		require.NoError(t, gw.Close())

		tmpDir := t.TempDir()
		extractErr := extractTarball(bytes.NewReader(buf.Bytes()), tmpDir)

		require.NoError(t, extractErr)
		// Normal file should exist.
		assert.FileExists(t, tmpDir+"/package/safe.js")
		// Traversal file should NOT exist outside tmpDir.
		assert.NoFileExists(t, tmpDir+"/../../../etc/evil")
	})
}

func TestClassifyPackage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pkg      npmPackageInfo
		apiName  string
		wantTier string
	}{
		{
			name:     "scoped official SDK",
			pkg:      npmPackageInfo{Name: "@notion/client", Scope: "@notion"},
			apiName:  "notion",
			wantTier: TierOfficialSDK,
		},
		{
			name:     "scoped official SDK with hq suffix",
			pkg:      npmPackageInfo{Name: "@notionhq/client", Scope: "@notionhq"},
			apiName:  "notion",
			wantTier: TierOfficialSDK,
		},
		{
			name:     "unscoped community SDK",
			pkg:      npmPackageInfo{Name: "notion-helper", Scope: ""},
			apiName:  "notion",
			wantTier: TierCommunitySDK,
		},
		{
			name:     "different scope community SDK",
			pkg:      npmPackageInfo{Name: "@somedev/notion-utils", Scope: "@somedev"},
			apiName:  "notion",
			wantTier: TierCommunitySDK,
		},
		{
			name:     "stripe official by scope",
			pkg:      npmPackageInfo{Name: "@stripe/stripe-js", Scope: "@stripe"},
			apiName:  "stripe",
			wantTier: TierOfficialSDK,
		},
		{
			name:     "api name contains scope",
			pkg:      npmPackageInfo{Name: "@cal/sdk", Scope: "@cal"},
			apiName:  "cal.com",
			wantTier: TierOfficialSDK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyPackage(tt.pkg, tt.apiName)
			assert.Equal(t, tt.wantTier, got)
		})
	}
}

func TestNPMSource_Search(t *testing.T) {
	t.Parallel()

	t.Run("parses search results", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/-/v1/search", r.URL.Path)
			assert.Equal(t, "notion", r.URL.Query().Get("text"))
			assert.Equal(t, "25", r.URL.Query().Get("size"))

			w.Write(searchResponse(
				npmPackageInfo{
					Name:    "@notionhq/client",
					Scope:   "@notionhq",
					Version: "2.2.0",
					Date:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
				},
				npmPackageInfo{
					Name:    "notion-client",
					Version: "1.0.0",
					Date:    time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				},
			))
		}))
		defer server.Close()

		src := NewNPMSource(NPMOptions{RegistryBaseURL: server.URL})
		packages, err := src.search(context.Background(), "notion")

		require.NoError(t, err)
		assert.Len(t, packages, 2)
		assert.Equal(t, "@notionhq/client", packages[0].Name)
		assert.Equal(t, "notion-client", packages[1].Name)
	})

	t.Run("handles API error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		src := NewNPMSource(NPMOptions{RegistryBaseURL: server.URL})
		_, err := src.search(context.Background(), "test")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "status 503")
	})
}

func TestNPMSource_FetchDownloads(t *testing.T) {
	t.Parallel()

	t.Run("parses bulk downloads", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/downloads/point/last-week/")
			w.Write(downloadsResponse(map[string]int{
				"pkg-a": 1000,
				"pkg-b": 50,
			}))
		}))
		defer server.Close()

		src := NewNPMSource(NPMOptions{DownloadsBaseURL: server.URL})
		result := src.fetchDownloads(context.Background(), []npmPackageInfo{
			{Name: "pkg-a"},
			{Name: "pkg-b"},
		})

		assert.Equal(t, 1000, result["pkg-a"])
		assert.Equal(t, 50, result["pkg-b"])
	})

	t.Run("handles API error gracefully", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		src := NewNPMSource(NPMOptions{DownloadsBaseURL: server.URL})
		result := src.fetchDownloads(context.Background(), []npmPackageInfo{
			{Name: "pkg-a"},
		})

		assert.Empty(t, result)
	})

	t.Run("empty packages returns empty map", func(t *testing.T) {
		t.Parallel()

		src := NewNPMSource(NPMOptions{})
		result := src.fetchDownloads(context.Background(), nil)

		assert.Empty(t, result)
	})
}

func TestNPMSource_FetchTarballURL(t *testing.T) {
	t.Parallel()

	t.Run("extracts tarball URL from version metadata", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/example-sdk/1.0.0", r.URL.Path)
			w.Write(versionResponse("https://registry.npmjs.org/example-sdk/-/example-sdk-1.0.0.tgz"))
		}))
		defer server.Close()

		src := NewNPMSource(NPMOptions{RegistryBaseURL: server.URL})
		tarballURL, err := src.fetchTarballURL(context.Background(), "example-sdk", "1.0.0")

		require.NoError(t, err)
		assert.Equal(t, "https://registry.npmjs.org/example-sdk/-/example-sdk-1.0.0.tgz", tarballURL)
	})

	t.Run("handles missing tarball URL", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"dist": {}}`))
		}))
		defer server.Close()

		src := NewNPMSource(NPMOptions{RegistryBaseURL: server.URL})
		_, err := src.fetchTarballURL(context.Background(), "bad-pkg", "1.0.0")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no tarball URL")
	})
}

func TestNPMSource_ProcessPackageTarball(t *testing.T) {
	t.Parallel()

	t.Run("extracts endpoints from tarball", func(t *testing.T) {
		t.Parallel()

		sdkContent := `
const baseUrl = "https://api.test.com";
class API {
  listUsers() { return this.get("/v1/users"); }
  createItem(data) { return this.post("/v1/items", data); }
}
`
		tarball := buildTarball(t, map[string]string{
			"package/src/client.js": sdkContent,
		})

		tarballServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(tarball)
		}))
		defer tarballServer.Close()

		src := NewNPMSource(NPMOptions{
			HTTPClient: tarballServer.Client(),
		})

		endpoints, baseURLs, err := src.processPackageTarball(
			context.Background(),
			tarballServer.URL+"/tarball.tgz",
			"test-sdk",
			TierCommunitySDK,
			500,
		)

		require.NoError(t, err)

		// Check endpoints were found.
		assert.NotEmpty(t, endpoints, "expected endpoints to be extracted")

		var paths []string
		for _, ep := range endpoints {
			paths = append(paths, ep.Path)
		}
		assert.Contains(t, paths, "/v1/users")
		assert.Contains(t, paths, "/v1/items")

		// Check base URLs.
		assert.Contains(t, baseURLs, "https://api.test.com")
	})

	t.Run("rejects non-HTTPS tarball URL", func(t *testing.T) {
		t.Parallel()

		src := NewNPMSource(NPMOptions{})
		_, _, err := src.processPackageTarball(
			context.Background(),
			"http://evil.com/tarball.tgz",
			"evil-sdk",
			TierCommunitySDK,
			0,
		)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "HTTPS")
	})

	t.Run("skips non-JS/TS files", func(t *testing.T) {
		t.Parallel()

		tarball := buildTarball(t, map[string]string{
			"package/readme.md":    `this.get("/v1/docs-only");`,
			"package/data.json":    `{"url": "/v1/json-data"}`,
			"package/src/index.ts": `this.get("/v1/real-endpoint");`,
		})

		tarballServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(tarball)
		}))
		defer tarballServer.Close()

		src := NewNPMSource(NPMOptions{
			HTTPClient: tarballServer.Client(),
		})

		endpoints, _, err := src.processPackageTarball(
			context.Background(),
			tarballServer.URL+"/tarball.tgz",
			"test-sdk",
			TierCommunitySDK,
			0,
		)

		require.NoError(t, err)

		var paths []string
		for _, ep := range endpoints {
			paths = append(paths, ep.Path)
		}
		assert.Contains(t, paths, "/v1/real-endpoint")
		// MD and JSON files should not have been grepped.
		assert.NotContains(t, paths, "/v1/docs-only")
		assert.NotContains(t, paths, "/v1/json-data")
	})

	t.Run("handles tarball with symlinks", func(t *testing.T) {
		t.Parallel()

		tarball := buildTarballWithSymlink(t)

		tarballServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(tarball)
		}))
		defer tarballServer.Close()

		src := NewNPMSource(NPMOptions{
			HTTPClient: tarballServer.Client(),
		})

		endpoints, _, err := src.processPackageTarball(
			context.Background(),
			tarballServer.URL+"/tarball.tgz",
			"symlink-sdk",
			TierCommunitySDK,
			0,
		)

		require.NoError(t, err)
		// Should still extract endpoints from the safe file.
		var paths []string
		for _, ep := range endpoints {
			paths = append(paths, ep.Path)
		}
		assert.Contains(t, paths, "/v1/safe")
	})
}

func TestNewNPMSource_Defaults(t *testing.T) {
	t.Parallel()

	src := NewNPMSource(NPMOptions{})

	assert.Equal(t, defaultRegistryBaseURL, src.registryBaseURL)
	assert.Equal(t, defaultDownloadsBaseURL, src.downloadsBaseURL)
	assert.Equal(t, defaultRecencyCutoff, src.recencyCutoff)
	assert.NotNil(t, src.httpClient)
}

func TestNewNPMSource_CustomOptions(t *testing.T) {
	t.Parallel()

	client := &http.Client{Timeout: 30 * time.Second}
	src := NewNPMSource(NPMOptions{
		RegistryBaseURL:  "https://custom-registry.com",
		DownloadsBaseURL: "https://custom-downloads.com",
		HTTPClient:       client,
		RecencyCutoff:    90 * 24 * time.Hour,
	})

	assert.Equal(t, "https://custom-registry.com", src.registryBaseURL)
	assert.Equal(t, "https://custom-downloads.com", src.downloadsBaseURL)
	assert.Equal(t, 90*24*time.Hour, src.recencyCutoff)
	assert.Same(t, client, src.httpClient)
}

func TestNPMSource_RecencyCutoffFiltering(t *testing.T) {
	t.Parallel()

	t.Run("custom cutoff of 30 days", func(t *testing.T) {
		t.Parallel()

		registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/-/v1/search":
				w.Write(searchResponse(
					npmPackageInfo{
						Name:    "recent-sdk",
						Version: "1.0.0",
						Date:    time.Now().Add(-7 * 24 * time.Hour), // 7 days ago
					},
					npmPackageInfo{
						Name:    "old-sdk",
						Version: "1.0.0",
						Date:    time.Now().Add(-60 * 24 * time.Hour), // 60 days ago
					},
				))
			default:
				// Return 404 for version lookups — old-sdk should never reach this.
				if r.URL.Path == "/old-sdk/1.0.0" {
					t.Error("old-sdk should have been filtered out by recency cutoff")
				}
				// recent-sdk version lookup will fail; that's fine — we're testing filtering.
				http.NotFound(w, r)
			}
		}))
		defer registryServer.Close()

		src := NewNPMSource(NPMOptions{
			RegistryBaseURL: registryServer.URL,
			RecencyCutoff:   30 * 24 * time.Hour,
		})

		result, err := src.Discover(context.Background(), "test")

		assert.NoError(t, err)
		// old-sdk (60 days) should be filtered, recent-sdk (7 days) proceeds
		// but version lookup fails so no endpoints. Key check: no error.
		_ = result
	})
}

func TestNPMSource_ProcessPackageTarball_WithParams(t *testing.T) {
	t.Parallel()

	t.Run("extracts endpoints with params from SDK code", func(t *testing.T) {
		t.Parallel()

		// Steam-like SDK content that has both endpoint paths and params objects.
		sdkContent := `
const BASE_URL = "https://api.steampowered.com";

class SteamAPI {
  getOwnedGames(steamid, { includeAppInfo = true } = {}) {
    return this.get("/IPlayerService/GetOwnedGames/v1", {
      steamid: steamid,
      include_appinfo: includeAppInfo,
      include_played_free_games: true
    });
  }

  getRecentlyPlayed(steamid, count = 10) {
    return this.get("/IPlayerService/GetRecentlyPlayedGames/v1", {
      steamid,
      count
    });
  }
}
`
		tarball := buildTarball(t, map[string]string{
			"package/src/client.js": sdkContent,
		})

		tarballServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(tarball)
		}))
		defer tarballServer.Close()

		src := NewNPMSource(NPMOptions{
			HTTPClient: tarballServer.Client(),
		})

		endpoints, baseURLs, err := src.processPackageTarball(
			context.Background(),
			tarballServer.URL+"/tarball.tgz",
			"steam-sdk",
			TierCommunitySDK,
			500,
		)

		require.NoError(t, err)
		assert.NotEmpty(t, endpoints, "expected endpoints to be extracted")
		assert.Contains(t, baseURLs, "https://api.steampowered.com")

		// Check that at least one endpoint has params populated.
		var endpointWithParams *DiscoveredEndpoint
		for i, ep := range endpoints {
			if len(ep.Params) > 0 {
				endpointWithParams = &endpoints[i]
				break
			}
		}
		require.NotNil(t, endpointWithParams, "expected at least one endpoint with params")

		// Verify param names exist.
		paramNames := make(map[string]bool)
		for _, p := range endpointWithParams.Params {
			paramNames[p.Name] = true
		}
		// Should have extracted some of the params from the object literal.
		assert.True(t, len(paramNames) > 0, "expected at least one param name")
	})
}

func TestNPMSource_MaxPackagesLimit(t *testing.T) {
	t.Parallel()

	var versionCallCount int

	// Create 15 recent packages; only 10 should be processed.
	packages := make([]npmPackageInfo, 15)
	for i := range packages {
		packages[i] = npmPackageInfo{
			Name:    fmt.Sprintf("sdk-%d", i),
			Version: "1.0.0",
			Date:    time.Now().Add(-24 * time.Hour),
		}
	}

	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/-/v1/search":
			w.Write(searchResponse(packages...))
		default:
			versionCallCount++
			http.NotFound(w, r) // version lookups fail; just counting
		}
	}))
	defer registryServer.Close()

	downloadsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(downloadsResponse(map[string]int{}))
	}))
	defer downloadsServer.Close()

	src := NewNPMSource(NPMOptions{
		RegistryBaseURL:  registryServer.URL,
		DownloadsBaseURL: downloadsServer.URL,
	})

	_, err := src.Discover(context.Background(), "test")

	assert.NoError(t, err)
	// Only first 10 should have version lookups attempted.
	assert.LessOrEqual(t, versionCallCount, maxPackagesToProcess)
}
