# Schedy

**Schedy** is a lightweight HTTP-based task scheduler that lets you schedule and execute tasks at specified times.

## Features

- Schedule tasks via HTTP API
- Executes tasks at the right time using Go routines and timers
- Simple and easy to integrate

## Getting Started

### Installation

```bash
git clone https://github.com/ksamirdev/schedy.git
cd schedy
go build -o schedy cmd/schedy/main.go
```

### Usage

Start the scheduler server:

```bash
./schedy
```

### API Endpoints

- `POST /tasks` — Schedule a new task

### Example

Schedule a task:

```bash
curl -X POST http://localhost:8080/tasks -d '{"execute_at":"2025-05-26T15:00:00Z","url":"https://webhook.site/8a741093-35cc-4085-9c0d-1e7f0c98ef9c", "payload": {"key": "value"}}' -H "Content-Type: application/json"
```
