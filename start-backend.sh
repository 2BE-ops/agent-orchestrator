#!/bin/bash
set -e

echo "🚀 Starting Cloud Backend Server"
echo "================================"
echo ""
echo "Environment:"
echo "  Database: postgres://aoadmin:aopass@localhost:5432/cloud"
echo "  Server: http://0.0.0.0:9090"
echo ""

cd /Users/nikhilachale/Desktop/agent-orchestrator/cloud

export POSTGRES_URL='postgres://aoadmin:aopass@localhost:5432/cloud'
export LOG_LEVEL='info'

echo "📦 Installing dependencies..."
go mod download 2>&1 | tail -5

echo ""
echo "🔨 Building server..."
go build -o bin/ao-cloud ./cmd/ao-cloud 2>&1 | tail -10

echo ""
echo "✅ Server built successfully!"
echo ""
echo "🎯 Starting server on http://0.0.0.0:9090"
echo "   Press Ctrl+C to stop"
echo ""

./bin/ao-cloud -addr=0.0.0.0:9090
