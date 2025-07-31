<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Support\Facades\DB;

return new class extends Migration
{
    /**
     * Run the migrations.
     */
    public function up(): void
    {
        // Create a function that sends notifications
        DB::statement("
            CREATE OR REPLACE FUNCTION notify_task_changes()
            RETURNS TRIGGER AS $$
            DECLARE
                notification json;
            BEGIN
                -- Build notification payload
                IF TG_OP = 'DELETE' THEN
                    notification = json_build_object(
                        'table', TG_TABLE_NAME,
                        'operation', TG_OP,
                        'old_data', row_to_json(OLD),
                        'timestamp', extract(epoch from now())
                    );
                ELSE
                    notification = json_build_object(
                        'table', TG_TABLE_NAME,
                        'operation', TG_OP,
                        'new_data', row_to_json(NEW),
                        'old_data', CASE WHEN TG_OP = 'UPDATE' THEN row_to_json(OLD) ELSE NULL END,
                        'timestamp', extract(epoch from now())
                    );
                END IF;
                
                -- Send notification on the channel
                PERFORM pg_notify('whagons_tasks_changes', notification::text);
                
                -- Return appropriate row
                IF TG_OP = 'DELETE' THEN
                    RETURN OLD;
                ELSE
                    RETURN NEW;
                END IF;
            END;
            $$ LANGUAGE plpgsql;
        ");

        // Create triggers for INSERT, UPDATE, DELETE on wh_tasks
        DB::statement("
            CREATE TRIGGER task_changes_trigger
            AFTER INSERT OR UPDATE OR DELETE ON wh_tasks
            FOR EACH ROW
            EXECUTE FUNCTION notify_task_changes();
        ");
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        DB::statement('DROP TRIGGER IF EXISTS task_changes_trigger ON wh_tasks;');
        DB::statement('DROP FUNCTION IF EXISTS notify_task_changes();');
    }
}; 