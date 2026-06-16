CREATE TABLE query_history (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  db_connection_id BIGINT UNSIGNED NOT NULL,
  db_connection_name VARCHAR(255) NOT NULL,
  database_name VARCHAR(255) NULL,
  schema_name VARCHAR(255) NULL,
  redis_db_index INT NULL,
  sql_content TEXT NOT NULL,
  duration_ms BIGINT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT fk_query_history_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_query_history_connection FOREIGN KEY (db_connection_id) REFERENCES db_connections(id) ON DELETE CASCADE,
  INDEX idx_query_history_user_created_at (user_id, created_at DESC)
);

CREATE TABLE saved_queries (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  label VARCHAR(255) NOT NULL,
  db_connection_id BIGINT UNSIGNED NOT NULL,
  db_connection_name VARCHAR(255) NOT NULL,
  database_name VARCHAR(255) NULL,
  schema_name VARCHAR(255) NULL,
  redis_db_index INT NULL,
  sql_content TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  CONSTRAINT fk_saved_queries_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_saved_queries_connection FOREIGN KEY (db_connection_id) REFERENCES db_connections(id) ON DELETE CASCADE,
  INDEX idx_saved_queries_user_updated_at (user_id, updated_at DESC)
);
