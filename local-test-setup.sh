#!/bin/bash
set -e

echo "🚀 Local Testing Setup for Cloud Agent Credentials"
echo "=================================================="

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
CLOUD_PORT=9090
FRONTEND_PORT=3000
DATA_DIR="$HOME/.ao-dev"
POSTGRES_PORT=5432

echo -e "${BLUE}Step 1: Checking prerequisites${NC}"
command -v docker &> /dev/null && echo "✅ Docker found" || echo "❌ Docker not found"
command -v go &> /dev/null && echo "✅ Go found" || echo "❌ Go not found"
command -v node &> /dev/null && echo "✅ Node found" || echo "❌ Node not found"

echo -e "\n${BLUE}Step 2: Setting up directories${NC}"
mkdir -p "$DATA_DIR/cloud"
mkdir -p "$DATA_DIR/frontend"
echo "✅ Created data directories at $DATA_DIR"

echo -e "\n${BLUE}Step 3: Starting PostgreSQL (Docker)${NC}"
docker rm -f ao-postgres 2>/dev/null || true
docker run -d \
  --name ao-postgres \
  -e POSTGRES_USER=aoadmin \
  -e POSTGRES_PASSWORD=aopass \
  -e POSTGRES_DB=cloud \
  -p "$POSTGRES_PORT:5432" \
  postgres:15-alpine
echo "✅ PostgreSQL started on port $POSTGRES_PORT"

echo -e "\n${BLUE}Step 4: Waiting for PostgreSQL to be ready${NC}"
sleep 5
until docker exec ao-postgres psql -U aoadmin -d cloud -c "SELECT 1" &> /dev/null; do
  echo "⏳ Waiting for PostgreSQL..."
  sleep 2
done
echo "✅ PostgreSQL is ready"

echo -e "\n${BLUE}Step 5: Running database migrations${NC}"
# This assumes migrations are in cloud/migrations
cd cloud
# go run ./cmd/migrate --up
echo "⚠️  Skipping migrations (configure as needed)"
cd ..

echo -e "\n${BLUE}Step 6: Starting Cloud Backend${NC}"
echo "📍 Backend URL: http://localhost:$CLOUD_PORT"
echo ""
echo "Run this in a new terminal:"
echo "  cd cloud"
echo "  POSTGRES_URL=postgres://aoadmin:aopass@localhost:5432/cloud go run ./cmd/server -addr=0.0.0.0:$CLOUD_PORT"
echo ""

echo -e "\n${BLUE}Step 7: Starting Frontend${NC}"
echo "📍 Frontend URL: http://localhost:$FRONTEND_PORT"
echo ""
echo "Run this in another new terminal:"
echo "  cd frontend"
echo "  npm install"
echo "  npm start"
echo ""

echo -e "\n${BLUE}Step 8: Configure Frontend Settings${NC}"
echo "Once frontend is running:"
echo "  1. Go to Settings → Cloud"
echo "  2. Set Control Plane URL: http://localhost:$CLOUD_PORT"
echo "  3. Authenticate (create account or login)"
echo ""

echo -e "\n${GREEN}✅ Setup complete!${NC}"
echo ""
echo "📋 Quick Test Checklist:"
echo "  □ Backend running on port $CLOUD_PORT"
echo "  □ Frontend running on port $FRONTEND_PORT"
echo "  □ PostgreSQL running on port $POSTGRES_PORT"
echo "  □ Frontend configured with cloud endpoint"
echo "  □ Create a cloud project"
echo "  □ Try creating a task (all agents should show but be disabled)"
echo "  □ Add credentials in Settings → Cloud"
echo "  □ Refresh - agents should now be enabled"
echo ""
echo "🔗 Useful commands:"
echo "  - View PostgreSQL: psql -h localhost -U aoadmin -d cloud"
echo "  - Check cloud logs: docker logs -f ao-postgres"
echo "  - Stop all: docker kill ao-postgres"
echo ""
