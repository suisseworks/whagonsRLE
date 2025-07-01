# Snapshot-Based Checksum Computation in PostgreSQL for Data Integrity

## Overview
This document details the implementation of **snapshot-based checksum computation** in PostgreSQL to verify data integrity between a backend database and a frontend IndexedDB cache in a real-time application. The checksum is computed using a consistent snapshot to avoid race conditions from concurrent updates, triggered by a frontend POST request for either a full-table or recent-500-rows checksum. Real-time `wal2json` updates are buffered during verification, and the last updated item in the snapshot is marked to ensure frontend consistency. The backend uses Go with `sockjs-go` for communication, integrating with the existing incremental hashing setup (`row_hashes` table).

## Requirements
- **Consistent Checksums**: Compute checksums on a fixed dataset snapshot to prevent mismatches from concurrent updates.
- **POST Request Trigger**: Frontend initiates checksum requests (full or recent-500 rows) via POST, not SockJS, for simplicity and security.
- **Buffering Updates**: Buffer real-time `wal2json` updates during verification to align frontend and backend datasets.
- **Last Updated Marker**: Identify the last updated item in the snapshot to ensure the frontend’s IndexedDB matches the snapshot’s state.
- **Efficiency**: Leverage incremental hashes in `row_hashes` for fast computation.
- **Integration**: Work with `wal2json`, Go, SockJS, and IndexedDB for real-time sync.

## Snapshot-Based Checksum Computation

### Concept
A snapshot-based checksum uses PostgreSQL’s transaction isolation (`REPEATABLE READ`) to compute a hash on a consistent dataset at a specific point in time, avoiding interference from concurrent updates. The snapshot captures:
- **Row Hashes**: Precomputed MD5 hashes from the `row_hashes` table for all rows or the recent 500 rows.
- **Last Updated Marker**: The `last_updated` timestamp and `row_id` of the most recent row in the snapshot to verify frontend alignment.
- **Changes Since Snapshot**: `wal2json` changes from the snapshot’s log sequence number (LSN) to the current LSN, sent to the frontend.

The frontend buffers real-time updates during verification, applies snapshot changes, and checks the last updated marker to confirm it matches the snapshot’s state before computing its checksum.

### Workflow
1. **Frontend POST Request**: Sends a POST request to `/api/checksum` with `{ table: "users", full: false }` (or `true` for full-table).
2. **Backend Snapshot**:
   - Starts a `REPEATABLE READ` transaction.
   - Records the snapshot LSN (`pg_current_wal_lsn()`).
   - Computes checksum from `row_hashes`.
   - Captures the last updated row’s `row_id` and `last_updated`.
   - Collects `wal2json` changes since the snapshot.
3. **Buffering Updates**: Frontend buffers real-time `wal2json` updates during verification.
4. **Frontend Verification**:
   - Applies snapshot changes.
   - Checks the last updated row matches the marker.
   - Computes checksum on IndexedDB.
   - Compares with backend checksum; retries with buffered updates if mismatched, or resyncs if needed.

## Backend Implementation (PostgreSQL and Go)

### 1. PostgreSQL: Snapshot Checksum Functions
Modify the existing checksum functions to include the last updated marker and changes since the snapshot.

**Recent 500 Rows Checksum**:
```sql
CREATE OR REPLACE FUNCTION compute_recent_500_checksum_with_changes(table_name TEXT)
RETURNS TABLE (
    checksum TEXT,
    last_row_id BIGINT,
    last_updated TIMESTAMP WITH TIME ZONE,
    changes_since JSONB
) AS $$
DECLARE
    snapshot_lsn TEXT;
    checksum_val TEXT;
    last_row_id_val BIGINT;
    last_updated_val TIMESTAMP WITH TIME ZONE;
BEGIN
    -- Start snapshot
    SET TRANSACTION ISOLATION LEVEL REPEATABLE READ;
    SELECT pg_current_wal_lsn() INTO snapshot_lsn;

    -- Compute checksum and get last row
    SELECT
        MD5(STRING_AGG(row_hash, '' ORDER BY row_id)),
        (SELECT row_id FROM row_hashes WHERE table_name = $1 ORDER BY last_updated DESC LIMIT 1),
        (SELECT last_updated FROM row_hashes WHERE table_name = $1 ORDER BY last_updated DESC LIMIT 1)
    INTO checksum_val, last_row_id_val, last_updated_val
    FROM (
        SELECT row_hash, row_id
        FROM row_hashes
        WHERE table_name = $1
        ORDER BY last_updated DESC
        LIMIT 500
    ) sub;

    -- Capture changes since snapshot
    WITH changes AS (
        SELECT jsonb_agg(change) AS change_list
        FROM (
            SELECT change
            FROM pg_logical_slot_peek_changes('my_slot', snapshot_lsn, NULL, 'format-version', '2')
            WHERE change->>'table' = $1
        ) sub
    )
    SELECT checksum_val, last_row_id_val, last_updated_val, COALESCE(changes.change_list, '[]'::jsonb)
    INTO checksum, last_row_id, last_updated, changes_since
    FROM changes;

    RETURN NEXT;
END;
$$ LANGUAGE plpgsql;
```

**Full-Table Checksum**:
```sql
CREATE OR REPLACE FUNCTION compute_full_checksum_with_changes(table_name TEXT)
RETURNS TABLE (
    checksum TEXT,
    last_row_id BIGINT,
    last_updated TIMESTAMP WITH TIME ZONE,
    changes_since JSONB
) AS $$
DECLARE
    snapshot_lsn TEXT;
    checksum_val TEXT;
    last_row_id_val BIGINT;
    last_updated_val TIMESTAMP WITH TIME ZONE;
BEGIN
    SET TRANSACTION ISOLATION LEVEL REPEATABLE READ;
    SELECT pg_current_wal_lsn() INTO snapshot_lsn;

    SELECT
        MD5(STRING_AGG(row_hash, '' ORDER BY row_id)),
        (SELECT row_id FROM row_hashes WHERE table_name = $1 ORDER BY last_updated DESC LIMIT 1),
        (SELECT last_updated FROM row_hashes WHERE table_name = $1 ORDER BY last_updated DESC LIMIT 1)
    INTO checksum_val, last_row_id_val, last_updated_val
    FROM row_hashes
    WHERE table_name = $1;

    WITH changes AS (
        SELECT jsonb_agg(change) AS change_list
        FROM (
            SELECT change
            FROM pg_logical_slot_peek_changes('my_slot', snapshot_lsn, NULL, 'format-version', '2')
            WHERE change->>'table' = $1
        ) sub
    )
    SELECT checksum_val, last_row_id_val, last_updated_val, COALESCE(changes.change_list, '[]'::jsonb)
    INTO checksum, last_row_id, last_updated, changes_since
    FROM changes;

    RETURN NEXT;
END;
$$ LANGUAGE plpgsql;
```

- **Logic**:
  - `REPEATABLE READ`: Ensures a consistent snapshot.
  - `pg_current_wal_lsn()`: Marks the snapshot’s LSN.
  - `row_hashes`: Computes MD5 checksum from stored hashes, sorted by `row_id`.
  - `last_row_id` and `last_updated`: Identifies the most recent row.
  - `pg_logical_slot_peek_changes`: Captures `wal2json` changes since the snapshot.
- **Prerequisites**:
  - Replication slot `my_slot` created: `CREATE_REPLICATION_SLOT my_slot TEMPORARY LOGICAL wal2json`.
  - `row_hashes` table and triggers from previous setup.
  - Indexes: `idx_row_hashes_table`, `idx_row_hashes_last_updated`.

### 2. Go Backend: POST Endpoint
Implement a POST endpoint to handle checksum requests, returning the checksum, last updated marker, and changes.

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "github.com/jackc/pgx/v5"
    "log"
    "net/http"
)

var db *pgx.Conn

func handleChecksumRequest(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var req struct {
        Table string `json:"table"`
        Full  bool   `json:"full"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }

    var query string
    if req.Full {
        query = "SELECT checksum, last_row_id, last_updated, changes_since FROM compute_full_checksum_with_changes($1)"
    } else {
        query = "SELECT checksum, last_row_id, last_updated, changes_since FROM compute_recent_500_checksum_with_changes($1)"
    }

    var checksum, lastRowID, lastUpdated, changes interface{}
    err := db.QueryRow(context.Background(), query, req.Table).Scan(&checksum, &lastRowID, &lastUpdated, &changes)
    if err != nil {
        http.Error(w, "Database error", http.StatusInternalServerError)
        log.Printf("Checksum query error: %v", err)
        return
    }

    response := map[string]interface{}{
        "type":         "checksum",
        "table":        req.Table,
        "checksum":     checksum,
        "last_row_id":  lastRowID,
        "last_updated": lastUpdated,
        "changes_since": changes,
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

func main() {
    var err error
    db, err = pgx.Connect(context.Background(), "your_connection_string")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close(context.Background())

    http.HandleFunc("/api/checksum", handleChecksumRequest)
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

- **Logic**:
  - Accept POST requests with JSON `{ table: "users", full: false }`.
  - Execute the appropriate checksum function.
  - Return JSON with checksum, `last_row_id`, `last_updated`, and `changes_since`.
- **Security**: Add JWT authentication middleware to secure the endpoint.

## Frontend Implementation (IndexedDB and SockJS)

### 1. Buffering Real-Time Updates
Buffer `wal2json` updates during checksum verification, triggered by the POST request response.

```javascript
const socket = new SockJS('http://your-server:8080/sockjs');
let updateBuffer = [];
let isVerifyingChecksum = false;

socket.onmessage = async (event) => {
    const msg = JSON.parse(event.data);
    if (msg.type === 'update' && isVerifyingChecksum) {
        updateBuffer.push(msg);
        return;
    }
    // Handle other updates normally
};
```

### 2. Compute Frontend Checksum
Compute the checksum in IndexedDB, applying `changes_since` and verifying the last updated marker.

```javascript
async function computeRowHash(row) {
    const rowData = `${row.id}${row.name}${row.last_updated}`;
    return md5(rowData); // md5.js
}

async function computeRecent500Checksum(db, storeName, changes = [], lastRowId, lastUpdated) {
    const tx = db.transaction([storeName, 'sync_metadata'], 'readwrite');
    const store = tx.objectStore(storeName);
    const metadataStore = tx.objectStore('sync_metadata');

    // Apply changes_since
    for (const change of changes) {
        if (change.kind === 'insert' || change.kind === 'update') {
            await store.put(change.columnvalues);
        } else if (change.kind === 'delete') {
            await store.delete(change.oldkeys.id);
        }
    }
    metadataStore.put({ key: 'last_sync', value: new Date().toISOString() });
    await tx.complete;

    // Verify last updated marker
    const tx2 = db.transaction([storeName], 'readonly');
    const index = tx2.objectStore(storeName).index('last_updated');
    const cursor = index.openCursor(null, 'prev');
    let matchesMarker = false;
    const rowHashes = [];
    let count = 0;

    return new Promise((resolve, reject) => {
        cursor.onsuccess = (event) => {
            const cursor = event.target.result;
            if (cursor && count < 500) {
                const row = cursor.value;
                if (count === 0 && row.id == lastRowId && row.last_updated === lastUpdated) {
                    matchesMarker = true;
                }
                rowHashes.push({ id: row.id, hash: computeRowHash(row) });
                count++;
                cursor.continue();
            } else {
                if (!matchesMarker) {
                    reject(new Error('Last updated marker mismatch'));
                    return;
                }
                rowHashes.sort((a, b) => a.id - b.id);
                const combined = rowHashes.map(r => r.hash).join('');
                resolve(md5(combined));
            }
        };
        cursor.onerror = () => reject(tx2.error);
    });
}

async function computeFullChecksum(db, storeName, changes = [], lastRowId, lastUpdated) {
    const tx = db.transaction([storeName, 'sync_metadata'], 'readwrite');
    const store = tx.objectStore(storeName);
    const metadataStore = tx.objectStore('sync_metadata');

    for (const change of changes) {
        if (change.kind === 'insert' || change.kind === 'update') {
            await store.put(change.columnvalues);
        } else if (change.kind === 'delete') {
            await store.delete(change.oldkeys.id);
        }
    }
    metadataStore.put({ key: 'last_sync', value: new Date().toISOString() });
    await tx.complete;

    const tx2 = db.transaction([storeName], 'readonly');
    const cursor = tx2.objectStore(storeName).openCursor();
    let matchesMarker = false;
    const rowHashes = [];

    return new Promise((resolve, reject) => {
        cursor.onsuccess = (event) => {
            const cursor = event.target.result;
            if (cursor) {
                const row = cursor.value;
                if (!matchesMarker && row.id == lastRowId && row.last_updated === lastUpdated) {
                    matchesMarker = true;
                }
                rowHashes.push({ id: row.id, hash: computeRowHash(row) });
                cursor.continue();
            } else {
                if (!matchesMarker) {
                    reject(new Error('Last updated marker mismatch'));
                    return;
                }
                rowHashes.sort((a, b) => a.id - b.id);
                const combined = rowHashes.map(r => r.hash).join('');
                resolve(md5(combined));
            }
        };
        cursor.onerror = () => reject(tx2.error);
    });
}
```

### 3. Verify Checksum with POST Request
Trigger checksum verification via POST, buffer updates, and handle retries or resync.

```javascript
async function verifyChecksum(tableName, full = false, retryCount = 0) {
    const maxRetries = 2;
    isVerifyingChecksum = true;
    updateBuffer = [];
    const db = await openIndexedDB();

    const response = await fetch('/api/checksum', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ table: tableName, full }),
    });
    const { checksum, last_row_id, last_updated, changes_since } = await response.json();

    try {
        const localChecksum = full
            ? await computeFullChecksum(db, tableName, changes_since, last_row_id, last_updated)
            : await computeRecent500Checksum(db, tableName, changes_since, last_row_id, last_updated);

        if (localChecksum !== checksum && retryCount < maxRetries) {
            // Apply buffered updates and retry
            for (const update of updateBuffer) {
                const tx = db.transaction([tableName, 'sync_metadata'], 'readwrite');
                const store = tx.objectStore(tableName);
                const metadataStore = tx.objectStore('sync_metadata');
                if (update.kind === 'insert' || update.kind === 'update') {
                    await store.put(update.data);
                } else if (update.kind === 'delete') {
                    await store.delete(update.data.id);
                }
                metadataStore.put({ key: 'last_sync', value: new Date().toISOString() });
                await tx.complete;
            }
            updateBuffer = [];
            isVerifyingChecksum = false;
            await verifyChecksum(tableName, full, retryCount + 1);
        } else if (localChecksum !== checksum) {
            console.log(`Checksum mismatch for ${tableName} after retries, initiating resync`);
            await resyncTable(tableName, full);
        } else {
            console.log(`Checksum match for ${tableName}`);
            // Apply buffered updates
            for (const update of updateBuffer) {
                const tx = db.transaction([tableName, 'sync_metadata'], 'readwrite');
                const store = tx.objectStore(tableName);
                const metadataStore = tx.objectStore('sync_metadata');
                if (update.kind === 'insert' || update.kind === 'update') {
                    await store.put(update.data);
                } else if (update.kind === 'delete') {
                    await store.delete(update.data.id);
                }
                metadataStore.put({ key: 'last_sync', value: new Date().toISOString() });
                await tx.complete;
            }
        }
    } catch (error) {
        if (error.message === 'Last updated marker mismatch') {
            console.log(`Last updated marker mismatch for ${tableName}, initiating resync`);
            await resyncTable(tableName, full);
        } else {
            throw error;
        }
    } finally {
        isVerifyingChecksum = false;
        updateBuffer = [];
    }
}

async function resyncTable(tableName, full) {
    const db = await openIndexedDB();
    const metadataTx = db.transaction(['sync_metadata'], 'readonly');
    const metadataStore = metadataTx.objectStore('sync_metadata');
    const lastSync = await metadataStore.get('last_sync')?.value || '1970-01-01T00:00:00Z';

    const response = await fetch(`/api/sync/${tableName}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ last_sync: lastSync }),
    });
    const { data } = await response.json();

    const tx = db.transaction([tableName, 'sync_metadata'], 'readwrite');
    const store = tx.objectStore(tableName);
    const metadataStore = tx.objectStore('sync_metadata');

    if (full) {
        await store.clear();
    }
    data.forEach(row => store.put(row));
    metadataStore.put({ key: 'last_sync', value: new Date().toISOString() });
    await tx.complete;
}
```

- **Logic**:
  - Send POST request to `/api/checksum`.
  - Buffer `wal2json` updates during verification (`isVerifyingChecksum