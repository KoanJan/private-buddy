<div align="center">
  <img src="web/public/favicon.svg" alt="Private Buddy" width="64">
  <h1>Private Buddy</h1>
</div>

A modern, private AI assistant system built from scratch. I initially called this project "Boring Practice" because it was just a practice project - I wanted to build a modern agent system from zero to show I know how to do it. I didn't plan to create anything fancy; I once just wanted to copy the features that other agent systems have, like watching a documentary - boring. However, recently I found something interesting — as an engineer without a background in cognitive science, psychology, or narratology, I discovered that through AI one can quickly judge whether certain theories have practical positive significance for the current business or engineering, and then apply them cross-disciplinarily, which was truly hard to imagine before.

---

## What It Does

You can:
- Create multiple AI agents with different prompts
- Chat with them and keep history
- Delegate tasks to agents for real-world execution

More features will be added gradually.

## Quick Start

### Prerequisites

- Python 3.11 or higher
- Node.js 18 or higher
- MySQL 8.0 or higher
- LLM API key

### 1. Clone the Repository

```bash
git clone https://github.com/KoanJan/private-buddy.git
cd private-buddy
```

### 2. Setup Server

```bash
cd server

# Setup environment (will use Python 3.11+ automatically)
./setup.sh

# Configure environment variables
cp .env.example .env
# Edit .env with your database and API configurations

# Initialize database
cd database
./init_db.sh
cd ..

# Start server
./start.sh
```

### 3. Setup Web

```bash
cd web

# Install dependencies
npm install

# Start development server
./start.sh
```

### 4. Access the Application

- **Web UI**: http://localhost:5173
- **API Documentation**: http://localhost:8000/docs
- **API ReDoc**: http://localhost:8000/redoc

## Service Management

Both server and web services include management scripts:

```bash
# Start service
./start.sh

# Stop service
./stop.sh

# Restart service
./restart.sh
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
