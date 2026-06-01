CREATE TABLE IF NOT EXISTS report_deliveries (
    provider TEXT NOT NULL,
    report_type TEXT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    destination TEXT NOT NULL,
    delivered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, report_type, period_start, period_end, destination)
);

CREATE INDEX IF NOT EXISTS idx_report_deliveries_delivered_at
    ON report_deliveries (delivered_at DESC);
