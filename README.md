# whagonsRLE

A Go implementation of real-time database synchronization using SockJS WebSockets. Built for **whagons** to maintain consistent sync between PostgreSQL tables and IndexedDB data with minimal data transfer for instant frontend querying and rendering.

## 🚀 Installation

### Standard Installation
```bash
go install github.com/suisseworks/whagonsRLE@latest
```

### If You Encounter Checksum Issues
For newly published modules, you may need to skip checksum verification:
```bash
# Option 1: Set as private module
go env -w GOPRIVATE=github.com/suisseworks/whagonsRLE
go install github.com/suisseworks/whagonsRLE@latest

# Option 2: Skip checksum verification
GOSUMDB=off go install github.com/suisseworks/whagonsRLE@latest
```

### Using Make Commands
```bash
# Normal installation
make install

# Skip checksum verification
make install-skip-checksum
```

## 📋 What It Does

- **Real-time sync** between PostgreSQL and browser IndexedDB
- **Minimal data transfer** - only sends changes, not full datasets
- **WebSocket-based** communication using SockJS for reliability
- **Instant frontend updates** without manual refreshing
- **Consistent data state** across database and frontend storage

## 🛠 Built With

- Go 1.24.3
- Fiber v2 (HTTP framework)
- SockJS-Go v3 (WebSocket implementation)
- PostgreSQL driver

---

**License:** See LICENSE file  
**Repository:** [github.com/suisseworks/whagonsRLE](https://github.com/suisseworks/whagonsRLE) 