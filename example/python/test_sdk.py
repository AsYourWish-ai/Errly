"""Errly Python SDK integration test example."""
import os

from errly import Errly

api_key = os.environ.get("ERRLY_API_KEY")
if not api_key:
    print("ERRLY_API_KEY is not set")
    exit(1)

client = Errly(
    url="http://localhost:5080",
    api_key=api_key,
    project="python-sdk-test",
    environment="test",
)

# Capture an exception
try:
    raise ValueError("SDK test error from Python")
except Exception as e:
    event_id = client.capture_exception(e)
    print(f"captured exception: {event_id}")

# Capture a message
client.capture_message("Python SDK integration test passed", level="info")
print("Python SDK test complete")

client.flush()
