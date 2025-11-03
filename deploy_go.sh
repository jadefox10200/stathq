#!/bin/bash
# Script to build the Go app and restart the server

# Build the Go application
go build -o stathq .

# Check if build was successful
if [ $? -eq 0 ]; then
    echo "Go build successful. Restarting server..."
    sudo systemctl restart stathq
    echo "Server restarted."
else
    echo "Go build failed."
    exit 1
fi