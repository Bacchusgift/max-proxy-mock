use crate::storage::Store;
use serde::{Deserialize, Serialize};
use std::process::Command;

const BACKUP_KEY: &str = "system_proxy_backup";
pub const PAC_URL: &str = "http://127.0.0.1:8900/proxy.pac";

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProxyStatus {
    pub supported: bool,
    pub os: String,
    pub service: String,
    pub enabled: bool,
    pub url: String,
    pub expected_url: String,
    pub managed: bool,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub message: String,
}

#[derive(Serialize, Deserialize)]
struct Backup {
    service: String,
    url: String,
    enabled: bool,
}

pub fn status() -> ProxyStatus {
    let mut result = ProxyStatus {
        supported: false,
        os: std::env::consts::OS.into(),
        service: String::new(),
        enabled: false,
        url: String::new(),
        expected_url: PAC_URL.into(),
        managed: false,
        message: String::new(),
    };
    if cfg!(not(target_os = "macos")) {
        result.message = "当前系统请使用 PAC 地址手动配置".into();
        return result;
    }
    match primary_service()
        .and_then(|service| auto_proxy(&service).map(|(enabled, url)| (service, enabled, url)))
    {
        Ok((service, enabled, url)) => {
            result.supported = true;
            result.service = service;
            result.enabled = enabled;
            result.managed = enabled && same_pac(&url, PAC_URL);
            result.url = url;
        }
        Err(error) => result.message = error,
    }
    result
}

pub fn enable(store: &Store) -> Result<ProxyStatus, String> {
    let before = status();
    if !before.supported {
        return Err(before.message);
    }
    if before.managed {
        return Ok(before);
    }
    let backup = Backup {
        service: before.service.clone(),
        url: before.url,
        enabled: before.enabled,
    };
    store
        .set_setting(
            BACKUP_KEY,
            &serde_json::to_string(&backup).map_err(|e| e.to_string())?,
        )
        .map_err(|e| e.to_string())?;
    run(
        "/usr/sbin/networksetup",
        &["-setautoproxyurl", &before.service, PAC_URL],
    )?;
    run(
        "/usr/sbin/networksetup",
        &["-setautoproxystate", &before.service, "on"],
    )?;
    let after = status();
    if !after.managed {
        return Err("系统没有接受 PAC 设置".into());
    }
    Ok(after)
}

pub fn restore(store: &Store) -> Result<ProxyStatus, String> {
    let current = status();
    if !current.supported {
        return Err(current.message);
    }
    if let Some(raw) = store.setting(BACKUP_KEY).map_err(|e| e.to_string())? {
        let backup: Backup = serde_json::from_str(&raw).map_err(|e| e.to_string())?;
        if !backup.url.is_empty() {
            run(
                "/usr/sbin/networksetup",
                &["-setautoproxyurl", &backup.service, &backup.url],
            )?;
        }
        run(
            "/usr/sbin/networksetup",
            &[
                "-setautoproxystate",
                &backup.service,
                if backup.enabled { "on" } else { "off" },
            ],
        )?;
    } else {
        run(
            "/usr/sbin/networksetup",
            &["-setautoproxystate", &current.service, "off"],
        )?;
    }
    store
        .delete_setting(BACKUP_KEY)
        .map_err(|e| e.to_string())?;
    Ok(status())
}

fn primary_service() -> Result<String, String> {
    let route = output("/sbin/route", &["-n", "get", "default"])?;
    let device = route
        .lines()
        .find_map(|line| {
            let mut fields = line.split_whitespace();
            (fields.next() == Some("interface:"))
                .then(|| fields.next().unwrap_or_default().to_string())
        })
        .filter(|v| !v.is_empty())
        .ok_or_else(|| "没有找到当前网络接口".to_string())?;
    let services = output("/usr/sbin/networksetup", &["-listnetworkserviceorder"])?;
    let mut service = String::new();
    for line in services.lines().map(str::trim) {
        if line.starts_with('(') && !line.starts_with("(Hardware") {
            if let Some(index) = line.find(')') {
                service = line[index + 1..].trim().trim_start_matches('*').to_string();
            }
        }
        if line.contains(&format!("Device: {device}")) && !service.is_empty() {
            return Ok(service);
        }
    }
    Err(format!("没有找到接口 {device} 对应的网络服务"))
}

fn auto_proxy(service: &str) -> Result<(bool, String), String> {
    let value = output("/usr/sbin/networksetup", &["-getautoproxyurl", service])?;
    let mut enabled = false;
    let mut url = String::new();
    for line in value.lines().map(str::trim) {
        if let Some(value) = line.strip_prefix("URL:") {
            url = value.trim().to_string();
        }
        if let Some(value) = line.strip_prefix("Enabled:") {
            enabled = value.trim().eq_ignore_ascii_case("yes");
        }
    }
    Ok((enabled, url))
}
fn output(program: &str, args: &[&str]) -> Result<String, String> {
    let output = Command::new(program)
        .args(args)
        .output()
        .map_err(|e| e.to_string())?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
        return Err(if stderr.is_empty() {
            if stdout.is_empty() {
                format!("系统命令执行失败：{program}")
            } else {
                stdout
            }
        } else {
            stderr
        });
    }
    Ok(String::from_utf8_lossy(&output.stdout).into_owned())
}
fn run(program: &str, args: &[&str]) -> Result<(), String> {
    output(program, args).map(|_| ())
}
fn same_pac(a: &str, b: &str) -> bool {
    a.trim().trim_end_matches('/') == b.trim().trim_end_matches('/')
}
