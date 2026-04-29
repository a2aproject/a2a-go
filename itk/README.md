# Running ITK Tests Locally

This directory contains scripts to run Integration Test Kit (ITK) tests locally using Podman.

## Prerequisites

### 1. Install Podman

Run the following commands to install Podman and its components:

```bash
sudo apt update && sudo apt install -y podman podman-docker podman-compose
```

### 2. Configure SubUIDs/SubGIDs

For rootless Podman to function correctly, you need to ensure subuids and subgids are configured for your user.

If they are not already configured, you can add them using (replace `$USER` with your username if needed):

```bash
sudo usermod --add-subuids 100000-165535 --add-subgids 100000-165535 $USER
```

After adding subuids or if you encounter permission issues, run:

```bash
podman system migrate
```

## Running Tests

### 1. Set Environment Variable

You must set the `A2A_SAMPLES_REVISION` environment variable to specify which revision of the `a2a-samples` repository to use for testing. This can be a branch name, tag, or commit hash.

Example:
```bash
export A2A_SAMPLES_REVISION=fix-go-current-agent-in-info-mode
```

### 2. Execute Tests

Run the test script from this directory:

```bash
./run_itk.sh
```

The script will:
- Clone `a2a-samples` (if not already present).
- Checkout the specified revision.
- Build the ITK service Docker image.
- Run the tests and output results.

## Debugging

To enable detailed debug logging and capture logs from the agents:

1. Set the `ITK_LOG_LEVEL` environment variable to `DEBUG`:
   ```bash
   export ITK_LOG_LEVEL=DEBUG
   ```

2. Run the tests as usual:
   ```bash
   ./run_itk.sh
   ```

When run with `DEBUG` level, the script will create a `logs` directory in this `itk` folder and mount it to the container. You can find detailed logs for each agent in the `logs/` directory.
