@echo off
REM Private Buddy Docker Deployment Script for Windows

echo =========================================
echo   Private Buddy Docker Deployment
echo =========================================
echo.

REM Check if Docker is installed
docker --version >nul 2>&1
if errorlevel 1 (
    echo Error: Docker is not installed
    echo Please install Docker Desktop first: https://docs.docker.com/desktop/install/windows-install/
    exit /b 1
)

REM Check if Docker Compose is installed
docker-compose --version >nul 2>&1
if errorlevel 1 (
    echo Error: Docker Compose is not installed
    echo Please install Docker Compose first: https://docs.docker.com/compose/install/
    exit /b 1
)

REM Check if .env exists, if not create from example
if not exist .env (
    if exist .env.example (
        echo Creating .env file from .env.example...
        copy .env.example .env
        echo ✓ .env file created
    ) else (
        echo Warning: .env.example not found, using default configuration
    )
) else (
    echo ✓ Using existing .env file
)

echo.
echo Building containers...
echo.

REM Build containers
docker-compose build

echo.
echo Starting containers...
echo.

REM Start containers
docker-compose up -d

echo.
echo =========================================
echo   Deployment Complete!
echo =========================================
echo.
echo Application is now running:
echo   - Web UI: http://localhost
echo   - API: http://localhost:8000
echo.
echo Data directory: C:\Users\%USERNAME%\PBD_trial_docker_and_embedding (inside container)
echo.
echo Useful commands:
echo   - View logs: docker-compose logs -f
echo   - Stop: docker-compose down
echo   - Restart: docker-compose restart
echo   - Rebuild: deploy-cn.bat
echo.

pause
