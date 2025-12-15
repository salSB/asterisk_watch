# asterisk_watch

A drop-in replacement for astmon for Asterisk versions 22 and above. 
Real-time monitoring of Asterisk 22+ systems via AMI.

---

## 📋 Overview

**asterisk_watch** monitors Asterisk PBX systems via the Asterisk Manager Interface (AMI) and provides:

- **Purpose:** Replace astmon to support Asterisk 22+, which uses `ExtensionStateList` instead of the deprecated `DataGet` command
- **Compatibility:** Asterisk 22+ only
- **Real-time Monitoring:** Live updates of queue statistics, extension states, and agent status
- **Data Persistence:** Redis for caching and InfluxDB for metrics storage

## ✨ Features

- **Real-time Queue Monitoring:**
    - Queue member statistics (calls taken, paused time, status)
    - Queue metrics (hold time, abandoned calls, service level)
    - Agent availability and pause reasons

- **Extension State Tracking:**
    - Monitor hint states
    - Real-time extension status updates

- **Metrics & Logging:**
    - InfluxDB integration for time-series data
    - Configurable logging levels (logrus)
    - Redis-backed data caching

- **Modern Architecture:**
    - Written in Go 1.24.10
    - Concurrent event processing
    - Automatic AMI reconnection

## 🏗️ Architecture

### Components

- **AMI Interface:** Connects to Asterisk Manager Interface for event streaming
- **Redis:** Stores real-time state and metrics (separate databases per organization)
- **InfluxDB:** Long-term metrics storage and historical data
- **HTTPS Server:** Serves web dashboard with TLS/SSL
- **ACME Server:** Handles Let's Encrypt certificate challenges (port 80)

### Data Flow

1. AMI events received from Asterisk
2. Events processed and cached in Redis
3. Metrics written to InfluxDB
4. Automatic refresh updates dashboard

## ⚙️ Configuration

Configuration details placeholder:

- /etc/asterisk_watch.yml (see asterisk_watch.yml.example)

## 🔧 Requirements

- Asterisk 22 or above

## 🏠 Habitat

**asterisk_watch** lives on server `dash.uc` at the configured port.
The application runs as a system service, continuously monitoring Asterisk instances and serving the web dashboard over HTTPS.

### Access Points

- **Web Dashboard:** `https://dash-uc-URL:<configured_port>`
- **ACME Challenges:** Port 89 
- **AMI Connection:** Connects to configured Asterisk server(s) on port 5038

### Service Location

- **Binary:** `/usr/local/bin/asterisk_watch` (or configured installation path)
- **Configuration:** `/etc/asterisk_watch.yml`
- **Systemd Service:** `asterisk_watch.service`
