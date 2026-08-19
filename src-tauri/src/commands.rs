use crate::{
    certificate::{self, CertificateStatus},
    core::Core,
    model::{normalize_domain, Endpoint, RecordingState},
    system_proxy,
};
use chrono::Utc;
use serde::Deserialize;
use serde_json::{json, Value};
use std::{collections::HashMap, sync::Arc};
use tauri::{AppHandle, Manager, State};

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ProjectInput {
    name: String,
    #[serde(default)]
    domain: String,
}
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct EndpointInput {
    #[serde(default = "default_method")]
    method: String,
    path: String,
    #[serde(default = "default_status")]
    status: u16,
    #[serde(default)]
    response_body: String,
}
#[derive(Deserialize)]
struct ProxyAction {
    action: String,
}
#[derive(Deserialize)]
struct MockInput {
    enabled: Option<bool>,
    status: Option<u16>,
    body: Option<String>,
    headers: Option<HashMap<String, String>>,
}
fn default_method() -> String {
    "GET".into()
}
fn default_status() -> u16 {
    200
}

#[tauri::command]
pub async fn api_call(
    core: State<'_, Arc<Core>>,
    method: String,
    path: String,
    body: Option<Value>,
) -> Result<Value, String> {
    let core = Arc::clone(&core);
    tauri::async_runtime::spawn_blocking(move || api_call_blocking(core, method, path, body))
        .await
        .map_err(|error| format!("本地命令执行失败: {error}"))?
}

fn api_call_blocking(
    core: Arc<Core>,
    method: String,
    path: String,
    body: Option<Value>,
) -> Result<Value, String> {
    let method = method.to_uppercase();
    let changed = method != "GET";
    let parts: Vec<&str> = path.trim_matches('/').split('/').collect();
    let result = match (method.as_str(), parts.as_slice()) {
        ("GET", ["api", "state"]) => {
            json!({"recording": core.recording(), "proxyAddress":"127.0.0.1:8899", "adminAddress":"desktop"})
        }
        ("GET", ["api", "projects"]) => {
            serde_json::to_value(core.store.projects().map_err(error)?).map_err(error)?
        }
        ("POST", ["api", "projects"]) => {
            let input: ProjectInput = parse(body)?;
            serde_json::to_value(
                core.store
                    .create_project(&input.name, &input.domain)
                    .map_err(error)?,
            )
            .map_err(error)?
        }
        ("PUT", ["api", "projects", id]) => {
            let input: ProjectInput = parse(body)?;
            core.store
                .update_project(id, &input.name, &input.domain)
                .map_err(error)?;
            json!({"ok":true})
        }
        ("DELETE", ["api", "projects", id]) => {
            core.store.delete_project(id).map_err(error)?;
            if core.recording().project_id == *id {
                core.set_recording(RecordingState::default());
            }
            json!({"ok":true})
        }
        ("GET", ["api", "projects", id, "endpoints"]) => {
            serde_json::to_value(core.store.endpoints(id).map_err(error)?).map_err(error)?
        }
        ("POST", ["api", "projects", id, "endpoints"]) => {
            let input: EndpointInput = parse(body)?;
            let project = core
                .store
                .projects()
                .map_err(error)?
                .into_iter()
                .find(|project| project.id == *id)
                .ok_or("项目不存在")?;
            let path = if input.path.starts_with('/') {
                input.path
            } else {
                format!("/{}", input.path)
            };
            let mut endpoint = Endpoint {
                id: String::new(),
                project_id: (*id).into(),
                method: input.method.to_uppercase(),
                scheme: "https".into(),
                host: project.domain,
                path,
                status: input.status,
                request_headers: HashMap::new(),
                request_body: String::new(),
                response_headers: HashMap::from([(
                    "Content-Type".into(),
                    "application/json; charset=utf-8".into(),
                )]),
                response_body: input.response_body,
                content_type: "application/json; charset=utf-8".into(),
                duration_ms: 0,
                source: "manual".into(),
                mocked: false,
                hit_count: 1,
                last_seen_at: Utc::now(),
            };
            core.store.upsert_endpoint(&mut endpoint).map_err(error)?;
            serde_json::to_value(endpoint).map_err(error)?
        }
        ("DELETE", ["api", "endpoints", id]) => {
            core.store.delete_endpoint(id).map_err(error)?;
            json!({"ok":true})
        }
        ("POST", ["api", "endpoints", id, "mock"]) => {
            serde_json::to_value(core.store.create_mock(id).map_err(error)?).map_err(error)?
        }
        ("GET", ["api", "mocks"]) => {
            serde_json::to_value(core.store.mocks().map_err(error)?).map_err(error)?
        }
        ("PATCH", ["api", "mocks", id]) => {
            let input: MockInput = parse(body)?;
            core.store
                .update_mock(
                    id,
                    input.enabled,
                    input.status,
                    input.body.as_deref(),
                    input.headers.as_ref(),
                )
                .map_err(error)?;
            json!({"ok":true})
        }
        ("DELETE", ["api", "mocks", id]) => {
            core.store.delete_mock(id).map_err(error)?;
            json!({"ok":true})
        }
        ("POST", ["api", "recording"]) => {
            let mut recording: RecordingState = parse(body)?;
            recording.domain = normalize_domain(&recording.domain);
            if recording.active && (recording.project_id.is_empty() || recording.domain.is_empty())
            {
                return Err("请选择项目并填写域名".into());
            }
            core.set_recording(recording.clone());
            serde_json::to_value(recording).map_err(error)?
        }
        ("GET", ["api", "system-proxy"]) => {
            serde_json::to_value(system_proxy::status()).map_err(error)?
        }
        ("POST", ["api", "system-proxy"]) => {
            let action: ProxyAction = parse(body)?;
            let status = match action.action.as_str() {
                "enable" => system_proxy::enable(&core.store),
                "restore" => system_proxy::restore(&core.store),
                _ => Err("未知操作".into()),
            }?;
            serde_json::to_value(status).map_err(error)?
        }
        _ => return Err(format!("未支持的本地接口: {method} {path}")),
    };
    if changed {
        core.emit("data");
    }
    Ok(result)
}

#[tauri::command]
pub fn reveal_certificate(app: AppHandle) -> Result<(), String> {
    let path = certificate_path(&app)?;
    std::process::Command::new("open")
        .args(["-R", path.to_string_lossy().as_ref()])
        .status()
        .map_err(error)?;
    Ok(())
}

#[tauri::command]
pub async fn certificate_status(app: AppHandle) -> Result<CertificateStatus, String> {
    let path = certificate_path(&app)?;
    tauri::async_runtime::spawn_blocking(move || certificate::status(&path))
        .await
        .map_err(|error| format!("证书状态检查失败：{error}"))
}

#[tauri::command]
pub async fn install_certificate(app: AppHandle) -> Result<CertificateStatus, String> {
    let path = certificate_path(&app)?;
    tauri::async_runtime::spawn_blocking(move || certificate::install(&path))
        .await
        .map_err(|error| format!("证书安装任务失败：{error}"))?
}

fn certificate_path(app: &AppHandle) -> Result<std::path::PathBuf, String> {
    Ok(app
        .path()
        .app_data_dir()
        .map_err(error)?
        .join("certificates/max-proxy-ca.crt"))
}

fn parse<T: for<'de> Deserialize<'de>>(body: Option<Value>) -> Result<T, String> {
    serde_json::from_value(body.unwrap_or(Value::Null)).map_err(|e| format!("请求内容无效: {e}"))
}
fn error(value: impl std::fmt::Display) -> String {
    value.to_string()
}
