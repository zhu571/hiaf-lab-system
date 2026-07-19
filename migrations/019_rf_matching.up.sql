CREATE TABLE rf_matching_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    device VARCHAR(16) NOT NULL
        CHECK (device IN ('rf_carpet','rfq','qpig')),
    frequency_mhz DOUBLE PRECISION NOT NULL,
    s11 DOUBLE PRECISION,
    input_freq DOUBLE PRECISION,
    input_voltage DOUBLE PRECISION,
    input_power DOUBLE PRECISION,
    input_desc TEXT NOT NULL DEFAULT '',
    output_freq DOUBLE PRECISION,
    output_voltage DOUBLE PRECISION,
    output_power DOUBLE PRECISION,
    output_desc TEXT NOT NULL DEFAULT '',
    transformer_turns VARCHAR(16) NOT NULL DEFAULT '',
    capacitance_text TEXT NOT NULL DEFAULT '',
    transformer_material TEXT NOT NULL DEFAULT '',
    shunt_inductance TEXT NOT NULL DEFAULT '',
    series_capacitor TEXT NOT NULL DEFAULT '',
    status VARCHAR(16)
        CHECK (status IN ('pass','adjust','fail')),
    notes TEXT NOT NULL DEFAULT '',
    measured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    measured_by UUID REFERENCES users(id) ON DELETE SET NULL,
    is_void BOOLEAN NOT NULL DEFAULT false,
    voided_at TIMESTAMPTZ,
    voided_by UUID REFERENCES users(id) ON DELETE SET NULL,
    void_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_rf_project ON rf_matching_records(project_id, device, measured_at DESC);
