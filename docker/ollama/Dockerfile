# Use the official Ollama base image
FROM ollama/ollama:latest

# Expose the port Cloud Run will use
EXPOSE 8080

# Set Ollama environment variables for host and port
ENV OLLAMA_HOST=0.0.0.0
ENV OLLAMA_PORT=8080

# --- Start of Model Pulling Section ---

# Start the Ollama server in the background and wait for it to be ready
# We use nohup to detach the process and background it.
# We also redirect output to a log file to avoid filling build logs with server output.
# Adjust the `sleep` duration if you find builds are flaky (e.g., increase to 15-20 seconds).
RUN nohup /bin/ollama serve > /tmp/ollama.log 2>&1 & \
    echo "Waiting for Ollama server to start..." && \
    sleep 10 && \
    echo "Ollama server should be ready. Pulling model 'gemma:2b-instruct'..." && \
    /bin/ollama pull gemma:2b-instruct && \
    echo "Model 'gemma:2b-instruct' pulled successfully."

# --- End of Model Pulling Section ---

# Start the Ollama server for the running container
CMD ["serve"]