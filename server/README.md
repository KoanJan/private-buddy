# Private Buddy Server

Private Buddy Server Service - A private AI chat assistant based on FastAPI.

## Tech Stack

- **Framework**: FastAPI 0.109.0
- **Database**: MySQL (SQLAlchemy ORM)
- **AI**: LangChain + OpenAI
- **Python**: 3.11+

## Quick Start

### 1. Environment Setup

Run the environment setup script:

```bash
./setup.sh
```

This script will:
- Create a Python virtual environment
- Install all dependencies
- Optionally install development dependencies

### 2. Configure Environment Variables

Copy the environment variable template:

```bash
cp .env.example .env
```

Edit the `.env` file to configure database connection and API keys:

```env
DATABASE_URL=mysql+pymysql://root@localhost:3306/private_buddy
SECRET_KEY=your-secret-key-here
```

### 3. Initialize Database

Run the database initialization script:

```bash
cd database
./init_db.sh
```

### 4. Start Service

```bash
./start.sh
```

The service will start at http://localhost:8000.

## Project Structure

```
server/
├── app/                    # Application main directory
│   ├── api/               # API routes
│   ├── models/            # Database models
│   ├── schemas/           # Pydantic models
│   ├── services/          # Business logic
│   ├── utils/             # Utility functions
│   ├── config.py          # Configuration management
│   ├── database.py        # Database connection
│   ├── logger.py          # Logging configuration
│   └── main.py            # Application entry point
├── database/              # Database scripts
│   ├── sql/              # SQL files
│   └── init_db.sh        # Initialization script
├── logs/                  # Log files
├── venv/                  # Virtual environment (auto-created)
├── pyproject.toml         # Project configuration and dependencies
├── setup.sh               # Environment setup script
├── start.sh               # Start script
├── stop.sh                # Stop script
└── restart.sh             # Restart script
```

## Dependency Management

### Core Dependencies

All core dependencies are defined in `[project.dependencies]` in `pyproject.toml`:

```bash
# Install core dependencies
pip install -e .
```

### Development Dependencies

Development dependencies are defined in `[project.optional-dependencies.dev]`:

```bash
# Install development dependencies
pip install -e .[dev]
```

### Adding New Dependencies

Edit `pyproject.toml` and add to the `dependencies` list:

```toml
[project]
dependencies = [
    "new-package==1.0.0",
]
```

Then reinstall:

```bash
pip install -e .
```

## Development Tools

### Code Formatting

Use Black for code formatting:

```bash
black app/
```

### Code Linting

Use Flake8 for code linting:

```bash
flake8 app/
```

### Type Checking

Use MyPy for type checking:

```bash
mypy app/
```

### Running Tests

Use Pytest to run tests:

```bash
pytest
```

## API Documentation

After starting the service, access the API documentation at:

- Swagger UI: http://localhost:8000/docs
- ReDoc: http://localhost:8000/redoc

## Service Management

### Start Service

```bash
./start.sh
```

### Stop Service

```bash
./stop.sh
```

### Restart Service

```bash
./restart.sh
```

## Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| DATABASE_URL | Database connection string | mysql+pymysql://root@localhost:3306/private_buddy |
| SECRET_KEY | Application secret key | your-secret-key-here |

## Logging

Log files are stored in the `logs/` directory, named by date:

```
logs/
├── app_20260418.log
└── app_20260419.log
```

## Database

### Table Structure

- **llm_configs**: LLM configuration table
- **agents**: Agent configuration table
- **sessions**: Session table
- **messages**: Message table

For detailed information, see [database/README.md](database/README.md)

## Troubleshooting

### Virtual Environment Issues

Recreate the virtual environment:

```bash
rm -rf venv
./setup.sh
```

### Database Connection Issues

1. Check if MySQL service is running
2. Check connection configuration in `.env` file
3. Check database user permissions

### Port Already in Use

Check if port 8000 is in use:

```bash
lsof -i :8000
```

## License

MIT License
