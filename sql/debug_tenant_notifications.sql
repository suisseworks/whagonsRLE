-- Debug script for tenant notification system
-- Run these queries on your landlord database to check the setup

-- 1. Check if the trigger function exists
SELECT 
    routine_name, 
    routine_type,
    routine_definition
FROM information_schema.routines 
WHERE routine_name = 'notify_tenant_changes';

-- 2. Check if the trigger exists on the tenants table
SELECT 
    trigger_name,
    event_manipulation,
    event_object_table,
    action_timing,
    action_statement
FROM information_schema.triggers 
WHERE event_object_table = 'tenants';

-- 3. Check current structure of tenants table
SELECT column_name, data_type, is_nullable
FROM information_schema.columns 
WHERE table_name = 'tenants'
ORDER BY ordinal_position;

-- 4. Test the notification manually (this should trigger a notification)
-- IMPORTANT: Only run this if you want to create a test tenant!
-- INSERT INTO tenants (name, domain, database) VALUES ('debug_test', 'debug.test.com', 'debug_db');

-- 5. Check PostgreSQL logs for any errors
-- Run this to see recent log entries (adjust path as needed):
-- SELECT * FROM pg_stat_activity WHERE application_name = 'psql';

-- 6. Test notification manually without inserting data
SELECT pg_notify(
    'tenant_changes', 
    '{"operation":"TEST","table":"tenants","message":"Manual test notification","timestamp":' || extract(epoch from now()) || '}'
);

-- 7. Check if there are any active listeners on the tenant_changes channel
SELECT 
    pid,
    application_name,
    client_addr,
    state,
    query
FROM pg_stat_activity 
WHERE query LIKE '%LISTEN%tenant_changes%' OR query LIKE '%pg_notify%tenant_changes%';

-- 8. Show recent tenants to verify table exists and has data
SELECT id, name, domain, database, created_at 
FROM tenants 
ORDER BY id DESC 
LIMIT 5; 