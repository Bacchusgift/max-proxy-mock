use crate::model::{normalize_domain, Endpoint, MockRule, Project};
use chrono::{DateTime, Utc};
use rusqlite::{params, Connection, OptionalExtension, Result};
use std::{collections::HashMap, path::Path, sync::Mutex};
use uuid::Uuid;

pub struct Store {
    connection: Mutex<Connection>,
}

impl Store {
    pub fn open(path: &Path) -> Result<Self> {
        let connection = Connection::open(path)?;
        connection.execute_batch(
            r#"
            PRAGMA journal_mode=WAL;
            PRAGMA foreign_keys=ON;
            CREATE TABLE IF NOT EXISTS projects (
              id TEXT PRIMARY KEY, name TEXT NOT NULL, domain TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
            );
            CREATE TABLE IF NOT EXISTS endpoints (
              id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
              method TEXT NOT NULL, scheme TEXT NOT NULL DEFAULT 'https', host TEXT NOT NULL DEFAULT '', path TEXT NOT NULL,
              status INTEGER NOT NULL DEFAULT 200, request_headers TEXT NOT NULL DEFAULT '{}', request_body TEXT NOT NULL DEFAULT '',
              response_headers TEXT NOT NULL DEFAULT '{}', response_body TEXT NOT NULL DEFAULT '', content_type TEXT NOT NULL DEFAULT '',
              duration_ms INTEGER NOT NULL DEFAULT 0, source TEXT NOT NULL DEFAULT 'recorded', hit_count INTEGER NOT NULL DEFAULT 1,
              last_seen_at TEXT NOT NULL, UNIQUE(project_id, path)
            );
            CREATE TABLE IF NOT EXISTS mock_rules (
              id TEXT PRIMARY KEY, endpoint_id TEXT NOT NULL UNIQUE REFERENCES endpoints(id) ON DELETE CASCADE,
              project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE, method TEXT NOT NULL, host TEXT NOT NULL,
              path TEXT NOT NULL, status INTEGER NOT NULL, headers TEXT NOT NULL DEFAULT '{}', body TEXT NOT NULL DEFAULT '',
              enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL
            );
            CREATE TABLE IF NOT EXISTS app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
            CREATE INDEX IF NOT EXISTS idx_endpoints_project_last_seen ON endpoints(project_id, last_seen_at DESC);
            CREATE INDEX IF NOT EXISTS idx_mock_rules_match ON mock_rules(host, path, enabled);
            "#,
        )?;
        Ok(Self {
            connection: Mutex::new(connection),
        })
    }

    pub fn import_legacy_if_empty(&self, legacy_path: &Path) -> Result<bool> {
        if !legacy_path.is_file() {
            return Ok(false);
        }
        let mut connection = self.connection.lock().unwrap();
        let project_count: i64 =
            connection.query_row("SELECT COUNT(*) FROM projects", [], |row| row.get(0))?;
        if project_count > 0 {
            return Ok(false);
        }
        connection.execute(
            "ATTACH DATABASE ? AS legacy",
            [legacy_path.to_string_lossy().as_ref()],
        )?;
        {
            let transaction = connection.transaction()?;
            transaction.execute(
                "INSERT OR IGNORE INTO projects SELECT * FROM legacy.projects",
                [],
            )?;
            transaction.execute(
                "INSERT OR IGNORE INTO endpoints SELECT * FROM legacy.endpoints",
                [],
            )?;
            transaction.execute(
                "INSERT OR IGNORE INTO mock_rules SELECT * FROM legacy.mock_rules",
                [],
            )?;
            transaction.commit()?;
        }
        connection.execute("DETACH DATABASE legacy", [])?;
        Ok(true)
    }

    pub fn projects(&self) -> Result<Vec<Project>> {
        let connection = self.connection.lock().unwrap();
        let mut statement = connection
            .prepare("SELECT id,name,domain,created_at FROM projects ORDER BY created_at")?;
        let projects = statement
            .query_map([], |row| {
                Ok(Project {
                    id: row.get(0)?,
                    name: row.get(1)?,
                    domain: row.get(2)?,
                    created_at: parse_time(row.get::<_, String>(3)?),
                })
            })?
            .collect();
        projects
    }

    pub fn create_project(&self, name: &str, domain: &str) -> Result<Project> {
        let name = name.trim();
        if name.is_empty() {
            return Err(rusqlite::Error::InvalidParameterName(
                "项目名称不能为空".into(),
            ));
        }
        let project = Project {
            id: id("prj"),
            name: name.into(),
            domain: normalize_domain(domain),
            created_at: Utc::now(),
        };
        self.connection.lock().unwrap().execute(
            "INSERT INTO projects(id,name,domain,created_at) VALUES(?,?,?,?)",
            params![
                project.id,
                project.name,
                project.domain,
                project.created_at.to_rfc3339()
            ],
        )?;
        Ok(project)
    }

    pub fn update_project(&self, id: &str, name: &str, domain: &str) -> Result<()> {
        let domain = normalize_domain(domain);
        let mut connection = self.connection.lock().unwrap();
        let transaction = connection.transaction()?;
        transaction.execute(
            "UPDATE projects SET name=?,domain=? WHERE id=?",
            params![name.trim(), domain, id],
        )?;
        transaction.execute(
            "UPDATE endpoints SET host=? WHERE project_id=?",
            params![domain, id],
        )?;
        transaction.execute(
            "UPDATE mock_rules SET host=? WHERE project_id=?",
            params![domain, id],
        )?;
        transaction.commit()
    }

    pub fn delete_project(&self, id: &str) -> Result<()> {
        self.connection
            .lock()
            .unwrap()
            .execute("DELETE FROM projects WHERE id=?", [id])?;
        Ok(())
    }

    pub fn endpoints(&self, project_id: &str) -> Result<Vec<Endpoint>> {
        let connection = self.connection.lock().unwrap();
        let mut statement = connection.prepare(
            "SELECT e.id,e.project_id,e.method,e.scheme,e.host,e.path,e.status,e.request_headers,e.request_body,e.response_headers,e.response_body,e.content_type,e.duration_ms,e.source,e.hit_count,e.last_seen_at,CASE WHEN m.id IS NULL THEN 0 ELSE 1 END FROM endpoints e LEFT JOIN mock_rules m ON m.endpoint_id=e.id WHERE e.project_id=? ORDER BY e.last_seen_at DESC"
        )?;
        let endpoints = statement.query_map([project_id], row_endpoint)?.collect();
        endpoints
    }

    pub fn endpoint(&self, id: &str) -> Result<Endpoint> {
        let connection = self.connection.lock().unwrap();
        connection.query_row(
            "SELECT e.id,e.project_id,e.method,e.scheme,e.host,e.path,e.status,e.request_headers,e.request_body,e.response_headers,e.response_body,e.content_type,e.duration_ms,e.source,e.hit_count,e.last_seen_at,CASE WHEN m.id IS NULL THEN 0 ELSE 1 END FROM endpoints e LEFT JOIN mock_rules m ON m.endpoint_id=e.id WHERE e.id=?",
            [id], row_endpoint,
        )
    }

    pub fn upsert_endpoint(&self, endpoint: &mut Endpoint) -> Result<()> {
        if endpoint.id.is_empty() {
            endpoint.id = id("ep");
        }
        let connection = self.connection.lock().unwrap();
        connection.execute(
            r#"INSERT INTO endpoints(id,project_id,method,scheme,host,path,status,request_headers,request_body,response_headers,response_body,content_type,duration_ms,source,hit_count,last_seen_at)
            VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?)
            ON CONFLICT(project_id,path) DO UPDATE SET method=excluded.method,scheme=excluded.scheme,host=excluded.host,status=excluded.status,
            request_headers=excluded.request_headers,request_body=excluded.request_body,response_headers=excluded.response_headers,response_body=excluded.response_body,
            content_type=excluded.content_type,duration_ms=excluded.duration_ms,hit_count=endpoints.hit_count+1,last_seen_at=excluded.last_seen_at"#,
            params![endpoint.id, endpoint.project_id, endpoint.method, endpoint.scheme, endpoint.host, endpoint.path, endpoint.status,
                encode_map(&endpoint.request_headers), endpoint.request_body, encode_map(&endpoint.response_headers), endpoint.response_body,
                endpoint.content_type, endpoint.duration_ms, endpoint.source, endpoint.last_seen_at.to_rfc3339()],
        )?;
        let (saved_id, hits, source): (String, i64, String) = connection.query_row(
            "SELECT id,hit_count,source FROM endpoints WHERE project_id=? AND path=?",
            params![endpoint.project_id, endpoint.path],
            |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?)),
        )?;
        endpoint.id = saved_id;
        endpoint.hit_count = hits;
        endpoint.source = source;
        Ok(())
    }

    pub fn delete_endpoint(&self, id: &str) -> Result<()> {
        self.connection
            .lock()
            .unwrap()
            .execute("DELETE FROM endpoints WHERE id=?", [id])?;
        Ok(())
    }

    pub fn mocks(&self) -> Result<Vec<MockRule>> {
        let connection = self.connection.lock().unwrap();
        let mut statement = connection.prepare("SELECT id,endpoint_id,project_id,method,host,path,status,headers,body,enabled,created_at FROM mock_rules")?;
        let mocks = statement
            .query_map([], |row| {
                Ok(MockRule {
                    id: row.get(0)?,
                    endpoint_id: row.get(1)?,
                    project_id: row.get(2)?,
                    method: row.get(3)?,
                    host: row.get(4)?,
                    path: row.get(5)?,
                    status: row.get(6)?,
                    headers: decode_map(row.get::<_, String>(7)?),
                    body: row.get(8)?,
                    enabled: row.get::<_, i64>(9)? == 1,
                    created_at: parse_time(row.get::<_, String>(10)?),
                })
            })?
            .collect();
        mocks
    }

    pub fn create_mock(&self, endpoint_id: &str) -> Result<MockRule> {
        let endpoint = self.endpoint(endpoint_id)?;
        let mock = MockRule {
            id: id("mock"),
            endpoint_id: endpoint.id,
            project_id: endpoint.project_id,
            method: endpoint.method,
            host: endpoint.host,
            path: endpoint.path,
            status: endpoint.status,
            headers: endpoint.response_headers,
            body: endpoint.response_body,
            enabled: true,
            created_at: Utc::now(),
        };
        self.connection.lock().unwrap().execute(
            "INSERT INTO mock_rules(id,endpoint_id,project_id,method,host,path,status,headers,body,enabled,created_at) VALUES(?,?,?,?,?,?,?,?,?,1,?) ON CONFLICT(endpoint_id) DO UPDATE SET method=excluded.method,host=excluded.host,path=excluded.path,status=excluded.status,headers=excluded.headers,body=excluded.body,enabled=1",
            params![mock.id,mock.endpoint_id,mock.project_id,mock.method,mock.host,mock.path,mock.status,encode_map(&mock.headers),mock.body,mock.created_at.to_rfc3339()],
        )?;
        Ok(mock)
    }

    pub fn update_mock(
        &self,
        id: &str,
        enabled: Option<bool>,
        status: Option<u16>,
        body: Option<&str>,
        headers: Option<&HashMap<String, String>>,
    ) -> Result<()> {
        let connection = self.connection.lock().unwrap();
        if let Some(value) = enabled {
            connection.execute(
                "UPDATE mock_rules SET enabled=? WHERE id=?",
                params![value as i64, id],
            )?;
        }
        if let Some(value) = status {
            connection.execute(
                "UPDATE mock_rules SET status=? WHERE id=?",
                params![value, id],
            )?;
        }
        if let Some(value) = body {
            connection.execute(
                "UPDATE mock_rules SET body=? WHERE id=?",
                params![value, id],
            )?;
        }
        if let Some(value) = headers {
            connection.execute(
                "UPDATE mock_rules SET headers=? WHERE id=?",
                params![encode_map(value), id],
            )?;
        }
        Ok(())
    }

    pub fn delete_mock(&self, id: &str) -> Result<()> {
        self.connection
            .lock()
            .unwrap()
            .execute("DELETE FROM mock_rules WHERE id=?", [id])?;
        Ok(())
    }

    pub fn setting(&self, key: &str) -> Result<Option<String>> {
        self.connection
            .lock()
            .unwrap()
            .query_row("SELECT value FROM app_settings WHERE key=?", [key], |row| {
                row.get(0)
            })
            .optional()
    }
    pub fn set_setting(&self, key: &str, value: &str) -> Result<()> {
        self.connection.lock().unwrap().execute("INSERT INTO app_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", params![key,value])?;
        Ok(())
    }
    pub fn delete_setting(&self, key: &str) -> Result<()> {
        self.connection
            .lock()
            .unwrap()
            .execute("DELETE FROM app_settings WHERE key=?", [key])?;
        Ok(())
    }
}

fn row_endpoint(row: &rusqlite::Row<'_>) -> Result<Endpoint> {
    Ok(Endpoint {
        id: row.get(0)?,
        project_id: row.get(1)?,
        method: row.get(2)?,
        scheme: row.get(3)?,
        host: row.get(4)?,
        path: row.get(5)?,
        status: row.get(6)?,
        request_headers: decode_map(row.get::<_, String>(7)?),
        request_body: row.get(8)?,
        response_headers: decode_map(row.get::<_, String>(9)?),
        response_body: row.get(10)?,
        content_type: row.get(11)?,
        duration_ms: row.get(12)?,
        source: row.get(13)?,
        hit_count: row.get(14)?,
        last_seen_at: parse_time(row.get::<_, String>(15)?),
        mocked: row.get::<_, i64>(16)? == 1,
    })
}
fn id(prefix: &str) -> String {
    format!("{prefix}_{}", Uuid::new_v4().simple())
}
fn parse_time(value: String) -> DateTime<Utc> {
    DateTime::parse_from_rfc3339(&value)
        .map(|v| v.with_timezone(&Utc))
        .unwrap_or_else(|_| Utc::now())
}
fn encode_map(value: &HashMap<String, String>) -> String {
    serde_json::to_string(value).unwrap_or_else(|_| "{}".into())
}
fn decode_map(value: String) -> HashMap<String, String> {
    serde_json::from_str(&value).unwrap_or_default()
}
