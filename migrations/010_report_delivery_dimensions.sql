ALTER TABLE report_deliveries
    ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT 'all';

ALTER TABLE report_deliveries
    ADD COLUMN IF NOT EXISTS report_type TEXT;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'report_deliveries'
          AND column_name = 'period'
    ) THEN
        UPDATE report_deliveries
        SET report_type = COALESCE(report_type, period)
        WHERE report_type IS NULL;
    ELSE
        UPDATE report_deliveries
        SET report_type = COALESCE(report_type, 'daily')
        WHERE report_type IS NULL;
    END IF;
END $$;

ALTER TABLE report_deliveries
    ALTER COLUMN report_type SET NOT NULL;

ALTER TABLE report_deliveries
    ADD COLUMN IF NOT EXISTS destination TEXT NOT NULL DEFAULT 'default';

ALTER TABLE report_deliveries
    DROP CONSTRAINT IF EXISTS report_deliveries_pkey;

ALTER TABLE report_deliveries
    ADD PRIMARY KEY (provider, report_type, period_start, period_end, destination);
