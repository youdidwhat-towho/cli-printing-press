package crowdsniff

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSpec(t *testing.T) {
	t.Parallel()

	t.Run("valid spec from aggregated endpoints", func(t *testing.T) {
		t.Parallel()
		endpoints := []AggregatedEndpoint{
			{Method: "GET", Path: "/v1/users", SourceTier: TierOfficialSDK, SourceCount: 2},
			{Method: "GET", Path: "/v1/users/{id}", SourceTier: TierOfficialSDK, SourceCount: 1},
			{Method: "POST", Path: "/v1/users", SourceTier: TierCommunitySDK, SourceCount: 1},
		}

		apiSpec, err := BuildSpec("notion", "https://api.notion.com", endpoints)
		require.NoError(t, err)

		assert.Equal(t, "notion", apiSpec.Name)
		assert.Equal(t, "https://api.notion.com", apiSpec.BaseURL)
		assert.NotEmpty(t, apiSpec.Resources)

		// Check that the "users" resource was created.
		users, ok := apiSpec.Resources["users"]
		require.True(t, ok)
		assert.Len(t, users.Endpoints, 3)
	})

	t.Run("meta contains source_tier and source_count", func(t *testing.T) {
		t.Parallel()
		endpoints := []AggregatedEndpoint{
			{Method: "GET", Path: "/v1/users", SourceTier: TierOfficialSDK, SourceCount: 3},
		}

		apiSpec, err := BuildSpec("test", "https://api.example.com", endpoints)
		require.NoError(t, err)

		for _, resource := range apiSpec.Resources {
			for _, ep := range resource.Endpoints {
				assert.Equal(t, "official-sdk", ep.Meta["source_tier"])
				assert.Equal(t, "3", ep.Meta["source_count"])
			}
		}
	})

	t.Run("resource grouping from paths", func(t *testing.T) {
		t.Parallel()
		endpoints := []AggregatedEndpoint{
			{Method: "GET", Path: "/v1/users", SourceTier: TierCodeSearch, SourceCount: 1},
			{Method: "GET", Path: "/v1/projects", SourceTier: TierCodeSearch, SourceCount: 1},
		}

		apiSpec, err := BuildSpec("test", "https://api.example.com", endpoints)
		require.NoError(t, err)

		_, hasUsers := apiSpec.Resources["users"]
		_, hasProjects := apiSpec.Resources["projects"]
		assert.True(t, hasUsers, "should have users resource")
		assert.True(t, hasProjects, "should have projects resource")
	})

	t.Run("empty endpoints returns error", func(t *testing.T) {
		t.Parallel()
		_, err := BuildSpec("test", "https://api.example.com", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints")
	})

	t.Run("empty name returns error", func(t *testing.T) {
		t.Parallel()
		_, err := BuildSpec("", "https://api.example.com", []AggregatedEndpoint{
			{Method: "GET", Path: "/users", SourceTier: TierCodeSearch, SourceCount: 1},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("empty base_url returns error", func(t *testing.T) {
		t.Parallel()
		_, err := BuildSpec("test", "", []AggregatedEndpoint{
			{Method: "GET", Path: "/users", SourceTier: TierCodeSearch, SourceCount: 1},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "base_url is required")
	})

	t.Run("spec passes validation", func(t *testing.T) {
		t.Parallel()
		endpoints := []AggregatedEndpoint{
			{Method: "GET", Path: "/users", SourceTier: TierCodeSearch, SourceCount: 1},
		}

		apiSpec, err := BuildSpec("test", "https://api.example.com", endpoints)
		require.NoError(t, err)
		assert.NoError(t, apiSpec.Validate())
	})
}

func TestResolveBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		explicit   string
		candidates []string
		want       string
	}{
		{
			name:       "explicit wins",
			explicit:   "https://explicit.com",
			candidates: []string{"https://candidate.com"},
			want:       "https://explicit.com",
		},
		{
			name:       "first candidate when no explicit",
			explicit:   "",
			candidates: []string{"https://first.com", "https://second.com"},
			want:       "https://first.com",
		},
		{
			name:       "skip empty candidates",
			explicit:   "",
			candidates: []string{"", " ", "https://valid.com"},
			want:       "https://valid.com",
		},
		{
			name:       "empty when nothing available",
			explicit:   "",
			candidates: nil,
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ResolveBaseURL(tt.explicit, tt.candidates))
		})
	}
}
