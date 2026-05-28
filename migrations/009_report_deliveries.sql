CREATE TABLE IF NOT EXISTS report_deliveries (
    period TEXT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    delivered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (period, period_start, period_end)
);

CREATE INDEX IF NOT EXISTS idx_report_deliveries_delivered_at
    ON report_deliveries (delivered_at DESC);
