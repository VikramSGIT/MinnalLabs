ALTER TABLE products
    DROP COLUMN IF EXISTS firmware_filename,
    DROP COLUMN IF EXISTS firmware_md5,
    DROP COLUMN IF EXISTS rollout_delay_days;

ALTER TABLE firmware_rollouts
    DROP COLUMN IF EXISTS firmware_filename,
    DROP COLUMN IF EXISTS firmware_md5;
