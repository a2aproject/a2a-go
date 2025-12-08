START TRANSACTION;

CREATE TABLE `task` (
    `id` CHAR(36) PRIMARY KEY,
    `created` TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    `last_updated` BIGINT,
    `state` VARCHAR(255),
    `user` VARCHAR(255),
    `task_json` TEXT
) ENGINE=InnoDB;

CREATE INDEX `idx_task_state_created` ON `task` (`state`, `created`);
CREATE INDEX `idx_task_user_last_updated` ON `task` (`user`, `last_updated`);

CREATE TABLE `task_event` (
    `id` CHAR(36) PRIMARY KEY,
    `created` TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    `last_updated` TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `type` VARCHAR(255),
    `task_id` CHAR(36),
    `task_version` BIGINT,
    `event_json` TEXT
) ENGINE=InnoDB;

CREATE INDEX `idx_task_event_task_id` ON `task_event` (`task_id`);

CREATE TABLE `task_execution` (
    `id` CHAR(36) PRIMARY KEY,
    `created` TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    `last_updated` TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `state` VARCHAR(255),
    `cause` VARCHAR(255),
    `work_type` VARCHAR(255),
    `worker_id` CHAR(36),
    `task_id` CHAR(36),
    `payload_json` TEXT
) ENGINE=InnoDB;

CREATE INDEX `idx_task_execution_task_id` ON `task_execution` (`task_id`);
CREATE INDEX `idx_task_execution_state_last_updated` ON `task_execution` (`state`, `last_updated`);

COMMIT;
