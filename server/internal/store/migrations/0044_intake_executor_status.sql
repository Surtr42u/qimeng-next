CREATE TABLE intake_executor_observations(
 task_id TEXT PRIMARY KEY REFERENCES delegated_tasks(task_id) ON DELETE CASCADE,
 data TEXT NOT NULL
);
