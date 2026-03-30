// Sample SDK source code for testing pattern extraction.

const BASE_URL = "https://api.example.com";

class ExampleClient {
  constructor(apiKey) {
    this.baseUrl = "https://api.example.com/v2";
    this.apiKey = apiKey;
  }

  async listUsers() {
    return this.get("/v1/users");
  }

  async getUser(userId) {
    return this.get(`/v1/users/${userId}`);
  }

  async createUser(data) {
    return this.post("/v1/users", data);
  }

  async updateProject(projectId, data) {
    return this.patch(`/v1/projects/${projectId}`, data);
  }

  async deleteItem(itemId) {
    return this.delete(`/v1/items/${itemId}`);
  }
}

// Fetch-based wrapper
async function fetchProjects() {
  return fetch("/v1/projects");
}

// Axios-style
const result = axios.get("/v1/teams");

// Request with explicit method
api.request({method: "PUT", url: "/v1/settings"});
