ALTER TABLE assembly_steps DROP COLUMN IF EXISTS source_template_id;
DROP TABLE IF EXISTS step_template_items;
DROP TABLE IF EXISTS step_templates;
