ALTER TABLE issues ADD COLUMN run_id UUID REFERENCES experiment_runs(id) ON DELETE SET NULL;
ALTER TABLE logs ADD COLUMN run_id UUID REFERENCES experiment_runs(id) ON DELETE SET NULL;
CREATE INDEX idx_issues_run ON issues(run_id) WHERE run_id IS NOT NULL;
CREATE INDEX idx_logs_run ON logs(run_id) WHERE run_id IS NOT NULL;
