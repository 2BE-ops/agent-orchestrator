# Local Testing Guide - Cloud Agent Credential Filtering

## Quick Start (5 minutes)

### Terminal 1: Start PostgreSQL
```bash
cd /Users/nikhilachale/Desktop/agent-orchestrator
docker-compose -f docker-compose.dev.yml up -d postgres

# Verify it's running
docker ps | grep ao-postgres
```

### Terminal 2: Start Cloud Backend
```bash
cd cloud
export POSTGRES_URL="postgres://aoadmin:aopass@localhost:5432/cloud"
go run ./cmd/server -addr=0.0.0.0:9090
```

**Expected output:**
```
Listening on http://0.0.0.0:9090
```

### Terminal 3: Start Frontend
```bash
cd frontend
npm install  # Only needed first time
npm start
```

**Expected output:**
```
➜ Local: http://localhost:3000
```

---

## Testing the Feature

### 1️⃣ **Create Cloud Project**
1. Open http://localhost:3000
2. Go to Dashboard → New Project
3. Select "Cloud" 
4. Fill in project details:
   - Name: "Local Test"
   - Repo: "https://github.com/user/test-repo"
   - Branch: "main"
5. Click "Create"

### 2️⃣ **Verify Agent Filtering (No Credentials)**
1. Open the project
2. Click "Create Task"
3. Look at Agent dropdown:
   - ✅ Shows all 3 agents (Claude Code, Codex, Cursor)
   - ❌ All are DISABLED
   - Shows "Needs auth" hint

**Screenshot check:**
```
Agent dropdown:
  - Claude Code [DISABLED] (Needs auth)
  - Codex [DISABLED] (Needs auth)
  - Cursor [DISABLED] (Needs auth)
```

### 3️⃣ **Add Credentials**
1. Go to Settings → Cloud → Add Credential
2. Select agent: "Claude Code"
3. Credential type: "Setup token"
4. Paste your setup token (or use test token)
5. Click "Add"

**Expected:** "Credential saved ✓"

### 4️⃣ **Verify Agent is Now Enabled**
1. Go back to project
2. Click "Create Task"
3. Check Agent dropdown again:
   - ✅ Claude Code: **ENABLED** ✓
   - ❌ Codex: DISABLED (Needs auth)
   - ❌ Cursor: DISABLED (Needs auth)

### 5️⃣ **Add More Credentials**
1. Repeat step 3 for Cursor agent
2. Use CURSOR_API_KEY from your cursor.sh account

### 6️⃣ **Final Test - Create Task with Cloud Agent**
1. Agent dropdown → Select "Claude Code" (now enabled)
2. Enter task: "List all files in the repo"
3. Click "Create Task"
4. Cloud session should start! 🚀

---

## API Testing

### Test Available Agents Endpoint

```bash
# Get your org ID and token first
ORG_ID="your-org-id"
TOKEN="your-bearer-token"

# Call the endpoint
curl -X GET "http://localhost:9090/api/cloud/v1/orgs/$ORG_ID/agents/available" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" | jq
```

**Expected response:**
```json
{
  "agents": [
    {
      "id": "claude-code",
      "provider": "claude-code",
      "hasValidCred": true,
      "validationState": "valid"
    },
    {
      "id": "codex",
      "provider": "codex",
      "hasValidCred": false,
      "validationState": "not_configured"
    },
    {
      "id": "cursor",
      "provider": "cursor",
      "hasValidCred": false,
      "validationState": "not_configured"
    }
  ]
}
```

### Test Credential Storage

```bash
# Add credential via API
curl -X PUT "http://localhost:9090/api/cloud/v1/orgs/$ORG_ID/provider-connections/agents/claude-code" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "credentialType": "oauth_token",
    "secret": "your-claude-code-token"
  }' | jq
```

---

## Database Inspection

### Connect to PostgreSQL
```bash
psql postgresql://aoadmin:aopass@localhost:5432/cloud
```

### Check Provider Connections
```sql
-- List all stored credentials
SELECT id, provider, label, validation_state, validated_at 
FROM provider_connections 
WHERE org_id = 'your-org-id';

-- Check specific agent
SELECT * FROM provider_connections 
WHERE org_id = 'your-org-id' AND provider = 'claude-code';
```

### Check Organizations
```sql
SELECT id, slug, display_name FROM organizations LIMIT 10;
```

---

## Debugging

### Frontend Console (DevTools)
```javascript
// Check React Query cache
window.UNSAFE_useQueryClient().getQueryData(['cloud', 'provider-connections', 'org-id'])

// Check stored credentials in memory
JSON.parse(localStorage.getItem('cloud-credentials'))
```

### Backend Logs
```bash
# Watch cloud service logs
docker logs -f ao-cloud

# Check PostgreSQL logs
docker logs -f ao-postgres
```

### Network Inspection
```bash
# Watch network requests
curl -v http://localhost:9090/api/cloud/v1/orgs/{orgId}/agents/available

# Use mitmproxy for detailed inspection
mitmproxy --mode transparent -p 8080
```

---

## Cleanup

### Stop all services
```bash
# Stop containers
docker-compose -f docker-compose.dev.yml down

# Or stop individual containers
docker kill ao-postgres ao-cloud
```

### Reset data
```bash
# Remove PostgreSQL volume (WARNING: erases all data)
docker volume rm agent-orchestrator_ao-postgres-data
```

---

## Troubleshooting

### PostgreSQL won't start
```bash
# Check if port is in use
lsof -i :5432

# Kill existing process
kill -9 <PID>

# Restart container
docker-compose -f docker-compose.dev.yml restart postgres
```

### Backend can't connect to PostgreSQL
```bash
# Verify connection string
POSTGRES_URL="postgres://aoadmin:aopass@localhost:5432/cloud"

# Test connection
psql $POSTGRES_URL -c "SELECT 1"
```

### Frontend can't reach backend
```bash
# Check backend is running
curl http://localhost:9090/api/cloud/v1/me

# Check CORS settings in cloud server
# Should allow requests from http://localhost:3000
```

### Agents still showing as disabled after adding credentials
```bash
# Clear browser cache
# Hard refresh: Cmd+Shift+R (Mac) or Ctrl+Shift+R (Windows)

# Check if credential was actually saved
curl http://localhost:9090/api/cloud/v1/orgs/{orgId}/provider-connections

# Verify token is valid
curl -X GET http://localhost:9090/api/cloud/v1/me \
  -H "Authorization: Bearer $TOKEN"
```

---

## Test Matrix

| Scenario | Expected | Status |
|----------|----------|--------|
| No credentials → all agents disabled | ❌ All disabled | Test #2 |
| Add Claude Code → Claude Code enabled | ✅ One enabled | Test #3-4 |
| Add Cursor → both enabled | ✅ Two enabled | Test #5 |
| Create task with enabled agent | Task starts | Test #6 |
| API endpoint returns correct status | Matches DB | API Test |
| Expired credential → shows disabled | ❌ Disabled | Manual |

---

## Test Credentials (Optional)

For testing without real credentials:

**Claude Code (mock):**
```
oauth_token: sk-test-claude-code-token-xxx
```

**Cursor (mock):**
```
api_key: test-cursor-api-key-xxx
```

**Codex (mock):**
```
access_token: test-codex-access-token-xxx
```

---

## Performance Tips

- Use `--dev` flag for backend (faster recompilation)
- Clear browser cache between tests
- Use `psql` directly for database tests (faster than curl)
- Watch logs in separate terminal window

---

**Ready to test?** Start with Terminal 1 commands above! 🚀
