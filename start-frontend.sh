#!/bin/bash
set -e

echo "🚀 Starting Frontend Development Server"
echo "========================================"
echo ""
echo "Frontend: http://localhost:3000"
echo "Backend API: http://localhost:9090"
echo ""

cd /Users/nikhilachale/Desktop/agent-orchestrator/frontend

echo "📦 Installing dependencies..."
npm install 2>&1 | grep -E "(added|up to date|npm WARN)" | tail -5

echo ""
echo "✅ Dependencies installed!"
echo ""
echo "🎯 Starting frontend on http://localhost:3000"
echo "   Press Ctrl+C to stop"
echo "   Auto-reload enabled"
echo ""

npm start
