# Private Buddy Server Dockerfile
FROM python:3.11-slim

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y \
    gcc \
    && rm -rf /var/lib/apt/lists/*

# Upgrade pip and configure mirror
RUN pip install --upgrade pip -i https://pypi.tuna.tsinghua.edu.cn/simple

# Copy project files
COPY pyproject.toml ./
COPY app ./app
COPY database ./database

# Install PyTorch CPU version first (smaller download, no CUDA dependencies)
RUN pip install torch --index-url https://download.pytorch.org/whl/cpu -i https://pypi.tuna.tsinghua.edu.cn/simple

# Install Python dependencies with mirror
RUN pip install -e . -i https://pypi.tuna.tsinghua.edu.cn/simple

# Pre-download embedding model (optional, reduces first-run latency)
RUN python -c "from chromadb.utils import embedding_functions; embedding_functions.SentenceTransformerEmbeddingFunction(model_name='BAAI/bge-base-zh')"

# Expose port
EXPOSE 8000

# Create data directory
RUN mkdir -p /root/PBD_trial_docker_and_embedding

# Run the application
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
