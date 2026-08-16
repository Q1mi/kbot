DROP TABLE IF EXISTS project_model_usage_reservations;
ALTER TABLE model_deployments DROP CONSTRAINT IF EXISTS model_deployment_prices_nonnegative;
ALTER TABLE model_deployments
    DROP COLUMN IF EXISTS cached_input_price_per_million,
    DROP COLUMN IF EXISTS output_price_per_million,
    DROP COLUMN IF EXISTS input_price_per_million;
