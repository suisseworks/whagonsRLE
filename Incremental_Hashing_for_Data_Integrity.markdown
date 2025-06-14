# Incremental Hashing for Data Integrity in Real-Time Database Synchronization

## Overview
This document details **incremental hashing** to ensure data integrity between a PostgreSQL backend database and a frontend IndexedDB cache in a real-time application. Incremental hashing stores precomputed row-level hashes in the backend, updated only when rows change, to enable fast checksum computation for verifying data consistency. The approach supports both full-table and recent-500-rows checksums, complementing `wal2json` for real-time updates and `last_updated`-based sync. The backend uses Go with `sockjs-go` to communicate checksums to the frontend via SockJS, where IndexedDB replicates the hashing logic to detect discrepancies.

## Requirements
- **Data Integrity**: Verify that the IndexedDB cache matches the PostgreSQL database, catching sync failures not detected by `last_updated`.
- **Efficiency**: Compute checksums quickly, especially for large tables, by avoiding redundant hashing of unchanged rows.
- **Flexibility**: Support checksums for all rows (full-table) and the most recent 500 rows (for frequent, lightweight checks).
- **Real-Time Compatibility**: Integrate with `wal2json` delta updates and SockJS communication.
- **Scalability**: Minimize database and client-side resource usage for frequent verification.

## What is Incremental Hashing?
Incremental hashing precomputes and stores a hash for each row in a dedicated table, updating hashes only when rows are inserted, updated, or deleted. These row hashes are aggregated into a single checksum for a table (or subset, e.g., recent 500 rows) when needed, avoiding the need to rehash entire tables. This is faster than on-demand hashing, especially for large datasets, as only changed rows trigger hash updates.

### How It Works
1. **Row-Level Hashing**:
   - Each row’s data (e.g., `id`, `name`, `last_updated`) is concatenated into a string.
   - A hash function (e.g., MD5) computes a fixed-size hash for the row.
   - Hashes are stored in a `row_hashes` table, keyed by table name and row ID.
2. **Incremental Updates**:
   - Triggers update the `row_hashes` table on `INSERT`, `UPDATE`, or `DELETE`.
   - Only modified rows have their hashes recomputed, reducing overhead.
3. **Checksum Computation**:
   - To verify a table, row hashes are retrieved (all or recent 500) and sorted by `row_id`.
   - Hashes are concatenated and hashed again to produce a single checksum.
4. **Frontend Replication**:
   - IndexedDB computes matching row hashes for objects in object stores.
   - Client aggregates hashes into a checksum, mirroring backend logic.
5. **Comparison**:
   - The client requests the backend checksum via SockJS.
   - If checksums mismatch, a resync (full or delta) is triggered to correct the IndexedDB cache.

## Backend Implementation (PostgreSQL and Go)

### 1. Create Hash Storage Table
Store row hashes in a dedicated table with indexes for fast retrieval.

```sql
CREATE TABLE row_hashes (
    table_name TEXT NOT NULL,
    row_id BIGINT NOT NULL,
    row_hash TEXT NOT NULL,
    last_updated TIMESTAMP WITH TIME ZONE NOT NULL,
    PRIMARY KEY (table_name, row_id)
);
CREATE INDEX idx_row_hashes_table ON row_hashes (table_name);
CREATE INDEX idx_row_hashes_last_updated ON row_hashes (table_name, last_updated DESC);
```

- **Columns**:
  - `table_name`: Identifies the source table (e.g., `users`, `orders`).
  - `row_id`: Matches the row’s primary key (`id`).
  - `row_hash`: MD5 hash of the row’s data.
  - `last_updated`: Timestamp from the source row for recent-row queries.
- **Indexes**:
  - `idx_row_hashes_table`: Speeds up full-table checksums.
  - `idx_row_hashes_last_updated`: Optimizes recent-500-rows queries.

### 2. Create Trigger Function
Update row hashes on `INSERT`, `UPDATE`, or `DELETE` using a trigger.

```sql
CREATE OR REPLACE FUNCTION update_row_hash()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        DELETE FROM row_hashes WHERE table_name = TG_TABLE_NAME AND row_id = OLD.id;
        RETURN OLD;
    ELSE
        INSERT INTO row_hashes (table_name, row_id, row_hash, last_updated)
        VALUES (TG_TABLE_NAME, NEW.id, MD5(CONCAT(NEW.id, NEW.name, NEW.last_updated)), NEW.last_updated)
        ON CONFLICT (table_name, row_id)
        DO UPDATE SET row_hash = EXCLUDED.row_hash, last_updated = EXCLUDED.last_updated;
        RETURN NEW;
    END IF;
END;
$$ LANGUAGE plpgsql;
```

- **Logic**:
  - On `DELETE`, remove the row’s hash.
  - On `INSERT` or `UPDATE`, compute MD5 of concatenated columns (adjust `CONCAT` to include all columns).
  - Use `ON CONFLICT` to update existing hashes.
- **Note**: Ensure `CONCAT` includes all columns (e.g., `NEW.id, NEW.name, NEW.last_updated`) in a consistent order, matching frontend logic.

### 3. Attach Triggers to Tables
Apply the trigger to each synced table (e.g., `users`, `orders`).

```sql
CREATE TRIGGER update_users_hash
AFTER INSERT OR UPDATE OR DELETE ON users
FOR EACH ROW
EXECUTE FUNCTION update_row_hash();

CREATE TRIGGER update_orders_hash
AFTER INSERT OR UPDATE OR DELETE ON orders
FOR EACH ROW
EXECUTE FUNCTION update_row_hash();
```

### 4. Compute Checksums
Create functions to compute checksums for all rows or the recent 500 rows.

**Full-Table Checksum**:
```sql
CREATE OR REPLACE FUNCTION compute_full_checksum(table_name TEXT) RETURNS TEXT AS $$
DECLARE
    checksum TEXT;
BEGIN
    SELECT MD5(STRING_AGG(row_hash, '' ORDER BY row_id))
    INTO checksum
    FROM row_hashes
    WHERE table_name = $1;
    RETURN checksum;
END;
$$ LANGUAGE plpgsql;
```

**Recent 500 Rows Checksum**:
```sql
CREATE OR REPLACE FUNCTION compute_recent_500_checksum(table_name TEXT) RETURNS TEXT AS $$
DECLARE
    checksum TEXT;
BEGIN
    SELECT MD5(STRING_AGG(row_hash, '' ORDER BY row_id))
    INTO checksum
    FROM (
        SELECT row_hash, row_id
        FROM row_hashes
        WHERE table_name = $1
        ORDER BY last_updated DESC
        LIMIT 500
    ) sub;
    RETURN checksum;
END;
$$ LANGUAGE plpgsql;
```

- **Logic**:
  - Retrieve row hashes, sorted by `row_id` for consistency.
  - Concatenate with `STRING_AGG` and hash with MD5.
  - `LIMIT 500` and `ORDER BY last_updated DESC` for recent rows.
- **Usage**:
  - `SELECT compute_full_checksum('users');` for all rows.
  - `SELECT compute_recent_500_checksum('users');` for recent 500 rows.

### 5. Go Backend Handler
Handle client checksum requests via SockJS, supporting full or recent-500 checksums.

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "github.com/igm/sockjs-go/v3/sockjs"
    "github.com/jackc/pgx/v5"
    "log"
    "net/http"
)

var db *pgx.Conn

func handleChecksumRequest(session sockjs.Session, msg string) {
    var req struct {
        Table string `json:"table"`
        Full  bool   `json:"full"`
    }
    if err := json.Unmarshal([]byte(msg), &req); err != nil {
        session.Send(`{"error":"Invalid request"}`)
        return
    }

    var query string
    if req.Full {
        query = "SELECT compute_full_checksum($1)"
    } else {
        query = "SELECT compute_recent_500_checksum($1)"
    }

    var checksum string
    err := db.QueryRow(context.Background(), query, req.Table).Scan(&checksum)
    if err != nil {
        session.Send(`{"error":"Database error"}`)
        log.Printf("Checksum query error: %v", err)
        return
    }

    session.Send(fmt.Sprintf(`{"type":"checksum","table":"%s","checksum":"%s"}`, req.Table, checksum))
}

func main() {
    var err error
    db, err = pgx.Connect(context.Background(), "your_connection_string")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close(context.Background())

    handler := sockjs.NewHandler("/sockjs", sockjs.DefaultOptions, func(session sockjs.Session) {
        for {
            if msg, err := session.Recv(); err == nil {
                handleChecksumRequest(session, msg)
            } else {
                break
            }
        }
    })

    http.Handle("/sockjs/", handler)
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

- **Logic**:
  - Parse client request for table name and full/recent flag.
  - Execute appropriate checksum query.
  - Send checksum to client via SockJS.
- **Security**: Add JWT authentication to `handleChecksumRequest` to secure access.

## Frontend Implementation (IndexedDB)

### 1. IndexedDB Schema Setup
Ensure object stores have a `last_updated` index for recent-500 queries.

```javascript
async function openIndexedDB() {
    return new Promise((resolve, reject) => {
        const request = indexedDB.open('myDatabase', 1);
        request.onupgradeneeded = (event) => {
            const db = event.target.result;
            const store = db.createObjectStore('users', { keyPath: 'id' });
            store.createIndex('last_updated', 'last_updated');
            db.createObjectStore('sync_metadata', { keyPath: 'key' });
        };
        request.onsuccess = (event) => resolve(event.target.result);
        request.onerror = (event) => reject(event.target.error);
    });
}
```

### 2. Compute Row Hashes and Checksums
Replicate backend hashing logic in IndexedDB, using MD5 for row data and aggregating for checksums.

**Dependencies**:
- Install MD5 library: `npm install md5` or use a CDN (e.g., `https://cdn.jsdelivr.net/npm/md5@2.3.0/md5.min.js`).

**Functions**:
```javascript
async function computeRowHash(row) {
    // Match backend CONCAT order
    const rowData = `${row.id}${row.name}${row.last_updated}`;
    return md5(rowData); // md5() from md5.js
}

async function computeFullChecksum(db, storeName) {
    const tx = db.transaction([storeName], 'readonly');
    const store = tx.objectStore(storeName);
    const cursor = store.openCursor();
    const rowHashes = [];

    return new Promise((resolve, reject) => {
        cursor.onsuccess = (event) => {
            const cursor = event.target.result;
            if (cursor) {
                rowHashes.push({ id: cursor.value.id, hash: computeRowHash(cursor.value) });
                cursor.continue();
            } else {
                // Sort by id for consistency
                rowHashes.sort((a, b) => a.id - b.id);
                const combined = rowHashes.map(r => r.hash).join('');
                resolve(md5(combined));
            }
        };
        cursor.onerror = () => reject(tx.error);
    });
}

async function computeRecent500Checksum(db, storeName) {
    const tx = db.transaction([storeName], 'readonly');
    const store = tx.objectStore(storeName);
    const index = store.index('last_updated');
    const cursor = index.openCursor(null, 'prev'); // Sort DESC
    const rowHashes = [];
    let count = 0;

    return new Promise((resolve, reject) => {
        cursor.onsuccess = (event) => {
            const cursor = event.target.result;
            if (cursor && count < 500) {
                rowHashes.push({ id: cursor.value.id, hash: computeRowHash(cursor.value) });
                count++;
                cursor.continue();
            } else {
                rowHashes.sort((a, b) => a.id - b.id);
                const combined = rowHashes.map(r => r.hash).join('');
                resolve(md5(combined));
            }
        };
        cursor.onerror = () => reject(tx.error);
    });
}
```

- **Logic**:
  - `computeRowHash`: Concatenates row data (matching backend `CONCAT`) and computes MD5.
  - `computeFullChecksum`: Hashes all objects, sorts by `id`, concatenates, and hashes.
  - `computeRecent500Checksum`: Hashes 500 most recent objects (by `last_updated`), sorts by `id`, concatenates, and hashes.
- **Consistency**: Ensure `rowData` matches backend’s `CONCAT` order and format (e.g., ISO 8601 for timestamps).

### 3. Verify Checksums
Request and compare checksums via SockJS, triggering resync on mismatch.

```javascript
const socket = new SockJS('http://your-server:8080/sockjs');

async function verifyChecksum(tableName, full = false) {
    const db = await openIndexedDB();
    const localChecksum = full
        ? await computeFullChecksum(db, tableName)
        : await computeRecent500Checksum(db, tableName);

    socket.send(JSON.stringify({ type: 'checksum', table: tableName, full }));

    socket.onmessage = async (event) => {
        const msg = JSON.parse(event.data);
        if (msg.type === 'checksum' && msg.table === tableName) {
            if (localChecksum !== msg.checksum) {
                console.log(`Checksum mismatch for ${tableName}, initiating resync`);
                await resyncTable(tableName, full);
            } else {
                console.log(`Checksum match for ${tableName}`);
            }
        }
    };
}

async function resyncTable(tableName, full) {
    const db = await openIndexedDB();
    const metadataTx = db.transaction(['sync_metadata'], 'readonly');
    const metadataStore = metadataTx.objectStore('sync_metadata');
    const lastSync = await metadataStore.get('last_sync')?.value || '1970-01-01T00:00:00Z';

    socket.send(JSON.stringify({ type: 'sync', table: tableName, last_sync: lastSync }));

    socket.onmessage = async (event) => {
        const msg = JSON.parse(event.data);
        if (msg.type === 'sync' && msg.table === tableName) {
            const tx = db.transaction([tableName, 'sync_metadata'], 'readwrite');
            const store = tx.objectStore(tableName);
            const metadataStore = tx.objectStore('sync_metadata');

            if (full) {
                await store.clear();
            }
            msg.data.forEach(row => store.put(row));
            metadataStore.put({ key: 'last_sync', value: new Date().toISOString() });
            await tx.complete;
        }
    };
}
```

- **Logic**:
  - Request full or recent-500 checksum via SockJS.
  - Compare with local checksum.
  - On mismatch, resync using delta updates (or full sync if specified).

## Integration with Existing Architecture
- **wal2json**: Continues to stream real-time delta updates, unaffected by hashing.
- **Go with sockjs-go**: Handles checksum requests alongside `wal2json` broadcasts.
- **IndexedDB**: Stores cached data and sync metadata; computes matching checksums.
- **SockJS Client**: Sends checksum/sync requests and receives responses.

## Performance and Scalability
- **Backend**:
  - Triggers update hashes incrementally, minimizing overhead.
  - Indexes on `row_hashes` ensure fast checksum queries.
  - `row_hashes` storage grows linearly with rows; prune old hashes periodically (e.g., `DELETE FROM row_hashes WHERE last_updated < NOW() - INTERVAL '1 year'`).
- **Frontend**:
  - IndexedDB indexes on `last_updated` speed up recent-500 queries.
  - Batch transactions minimize overhead during resync.
- **Optimization**:
  - Cache checksums in backend memory (e.g., Redis) for frequent requests.
  - Adjust 500-row limit or use time-based windows (e.g., last 24 hours) per table.

## Challenges and Mitigations
- **Storage Overhead**: `row_hashes` table grows with row count.
  - **Mitigation**: Prune old hashes; use efficient storage (MD5 is 32 bytes).
- **Consistency**: Mismatched column order or formats cause false mismatches.
  - **Mitigation**: Standardize `CONCAT` and `rowData` (e.g., ISO 8601 timestamps).
- **Performance**: Large tables slow full checksums.
  - **Mitigation**: Rely on recent-500 checksums for frequent checks; run full checksums weekly.
- **Security**: Unauthenticated checksum requests risk data exposure.
  - **Mitigation**: Use JWT in `handleChecksumRequest` and HTTPS/WSS.

## Best Practices
- **Frequent Recent Checks**: Run recent-500 checksums on reconnect or hourly.
- **Periodic Full Checks**: Run full-table checksums weekly or on manual trigger.
- **Row Count Check**: Compare row counts before checksums to catch gross errors.
- **Logging**: Log mismatches and resyncs for debugging.
- **Schema Changes**: Update `CONCAT` in trigger and frontend when columns change.
- **Testing**: Simulate sync failures to verify checksum detection and resync.

## Conclusion
Incremental hashing stores row-level MD5 hashes in PostgreSQL, updated via triggers, to enable fast checksum computation for full-table or recent-500-rows verification. The frontend replicates this in IndexedDB, ensuring data integrity by comparing checksums. This approach is efficient, scalable, and integrates seamlessly with your `wal2json` and SockJS-based architecture, catching sync discrepancies not detected by `last_updated`.