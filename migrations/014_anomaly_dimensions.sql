ALTER TABLE cost_anomalies
    ADD COLUMN IF NOT EXISTS compartment_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS compartment_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS currency TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS direction TEXT NOT NULL DEFAULT '';

-- Anomalies are derived data. Existing rows were calculated without the
-- dimensions above and cannot be backfilled reliably.
DELETE FROM cost_anomalies;

CREATE INDEX IF NOT EXISTS idx_cost_anomalies_compartment_range
    ON cost_anomalies (compartment_name, period_start DESC);
