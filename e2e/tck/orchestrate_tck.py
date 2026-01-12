# Copyright 2026 The A2A Authors

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import time
import requests
import subprocess
import os           
import sys

TCK_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), "../../../a2a-tck"))
SUT_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), "."))
TCK_VENV_PYTHON = os.path.join(TCK_DIR, ".venv", "bin", "python")
SUT_URL = "http://localhost:9999"

def wait_for_server(url, expected_status=200, timeout=120, interval=2):
    start_time = time.time()
    print(f"⏳ Waiting for server at: {url}")

    while True:
        elapsed_time = time.time() - start_time
        
        if elapsed_time >= timeout:
            print(f"❌ Timeout: Server did not respond with {expected_status} within {timeout}s.")
            sys.exit(1)

        try:
            response = requests.get(url, timeout=5)
            status_code = response.status_code
        except requests.exceptions.RequestException:
            status_code = "No Response"

        if status_code == expected_status:
            print(f"✅ Server is up! Received status {status_code} after {int(elapsed_time)}s.")
            return True

        print(f"⏳ Status: {status_code}. Retrying in {interval}s...")
        time.sleep(interval)

def setup_tck_env():
    print("Setting up TCK environment...")
    if not os.path.exists(TCK_DIR):
        print("TCK directory not found")
        sys.exit(1)
    
    run_shell_command("curl -LsSf https://astral.sh/uv/install.sh | sh", cwd=TCK_DIR)
    run_shell_command("uv venv --clear", cwd=TCK_DIR)
    run_shell_command("uv pip install -e .", cwd=TCK_DIR)

def run_shell_command(command, cwd=None):
    env = os.environ.copy()
    venv_bin = os.path.dirname(TCK_VENV_PYTHON)
    env["PATH"] = venv_bin + os.pathsep + env.get("PATH", "")
    env["UV_INDEX_URL"] = "https://pypi.org/simple"

    result = subprocess.run(command, shell=True, cwd=cwd, env=env, check=True)

def start_and_test(protocol):
    sut_process = subprocess.Popen(
        ["go", "run", ".", "--mode", protocol], 
        cwd=SUT_DIR,
    )
    time.sleep(1) 
    if sut_process.poll() is not None:
        print("❌ Critical Error: The Go SUT failed to start immediately.")
        sys.exit(1)

    card_url = f"{SUT_URL}/.well-known/agent-card.json"
    if not wait_for_server(card_url):
        print("Server failed to start")
        return False

    categories = ["mandatory", "capabilities"]

    try:
        for category in categories:
            print(f"Running TCK ({category}) for {protocol}...")
            run_shell_command(
                f"./run_tck.py --sut-url {SUT_URL} --category {category} --transports {protocol}",
                cwd=TCK_DIR
            )
        return True
    except Exception as e:
        print(f"❌ Error running TCK: {e}")
        return False
    finally:
        print("🛑 Stopping SUT...")
        sut_process.terminate()
        sut_process.wait(timeout=5)
        run_shell_command("fuser -k 9999/tcp || true", cwd=SUT_DIR)

def main():
    setup_tck_env()
    protocols = ["jsonrpc", "grpc"] 
    failed = []
    for protocol in protocols:
        if not start_and_test(protocol):
            failed.append(protocol)
    if not failed:
        print("✅ TCK passed for all protocols")
        return
    print(f"❌ TCK failed for protocols: {failed}")
    sys.exit(1)

if __name__ == "__main__":
    main()