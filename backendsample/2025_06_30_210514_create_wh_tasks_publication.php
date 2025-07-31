<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;
use Illuminate\Support\Facades\DB;

return new class extends Migration
{
    /**
     * Run the migrations.
     */
    public function up(): void
    {
        // Create publication for wh_tasks table to enable logical replication
        // This enables data integrity through read replicas, backups, and data warehouses
        DB::statement('CREATE PUBLICATION whagons_tasks_changes FOR TABLE wh_tasks;');
        
        // Note: This publication works alongside the trigger-based NOTIFY system
        // - Publication: Enables database replication for data integrity
        // - NOTIFY triggers: Enable real-time application notifications
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        // Drop the publication for logical replication
        DB::statement('DROP PUBLICATION IF EXISTS whagons_tasks_changes;');
    }
};
