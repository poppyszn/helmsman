# Playwright with socat, so that inside the test container localhost:5173 reaches the dev web server
# and localhost:8090 reaches fake-github. The sign-in flow sends the browser to those exact addresses.
FROM mcr.microsoft.com/playwright:v1.65.0-noble
RUN apt-get update && apt-get install -y --no-install-recommends socat && rm -rf /var/lib/apt/lists/*
