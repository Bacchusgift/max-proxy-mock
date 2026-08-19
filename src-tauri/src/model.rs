use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Project {
    pub id: String,
    pub name: String,
    pub domain: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Endpoint {
    pub id: String,
    pub project_id: String,
    pub method: String,
    pub scheme: String,
    pub host: String,
    pub path: String,
    pub status: u16,
    pub request_headers: HashMap<String, String>,
    pub request_body: String,
    pub response_headers: HashMap<String, String>,
    pub response_body: String,
    pub content_type: String,
    pub duration_ms: i64,
    pub source: String,
    pub mocked: bool,
    pub hit_count: i64,
    pub last_seen_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MockRule {
    pub id: String,
    pub endpoint_id: String,
    pub project_id: String,
    pub method: String,
    pub host: String,
    pub path: String,
    pub status: u16,
    pub headers: HashMap<String, String>,
    pub body: String,
    pub enabled: bool,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RecordingState {
    pub active: bool,
    pub project_id: String,
    pub domain: String,
}

pub fn normalize_domain(value: &str) -> String {
    let value = value.trim().to_lowercase();
    let value = value
        .strip_prefix("https://")
        .or_else(|| value.strip_prefix("http://"))
        .unwrap_or(&value);
    value
        .split('/')
        .next()
        .unwrap_or_default()
        .split(':')
        .next()
        .unwrap_or_default()
        .trim_end_matches('.')
        .to_string()
}

pub fn domain_matches(host: &str, domain: &str) -> bool {
    let host = normalize_domain(host);
    let domain = normalize_domain(domain);
    !domain.is_empty() && (host == domain || host.ends_with(&format!(".{domain}")))
}
