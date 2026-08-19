use crate::core::Core;
use axum::{
    extract::State,
    http::{header, HeaderMap},
    response::IntoResponse,
    routing::get,
    Router,
};
use std::{net::SocketAddr, sync::Arc};

pub async fn start(core: Arc<Core>) -> Result<(), String> {
    let app = Router::new()
        .route("/proxy.pac", get(pac))
        .route("/health", get(|| async { "ok" }))
        .with_state(core);
    let listener = tokio::net::TcpListener::bind(SocketAddr::from(([127, 0, 0, 1], 8900)))
        .await
        .map_err(|e| e.to_string())?;
    axum::serve(listener, app).await.map_err(|e| e.to_string())
}
async fn pac(State(core): State<Arc<Core>>) -> impl IntoResponse {
    let mut rules = String::new();
    for project in core.store.projects().unwrap_or_default() {
        if project.domain.is_empty() {
            continue;
        }
        let domain = serde_json::to_string(&project.domain.to_lowercase()).unwrap_or_default();
        rules.push_str(&format!("  if (host === {domain} || dnsDomainIs(host, \".\" + {domain})) return \"PROXY 127.0.0.1:8899\";\n"));
    }
    let body = format!("function FindProxyForURL(url, host) {{\n  host = host.toLowerCase();\n  if (isPlainHostName(host) || host === \"localhost\" || shExpMatch(host, \"127.*\")) return \"DIRECT\";\n{rules}  return \"DIRECT\";\n}}\n");
    let mut headers = HeaderMap::new();
    headers.insert(
        header::CONTENT_TYPE,
        "application/x-ns-proxy-autoconfig; charset=utf-8"
            .parse()
            .unwrap(),
    );
    headers.insert(
        header::CACHE_CONTROL,
        "no-store, max-age=0".parse().unwrap(),
    );
    (headers, body)
}
