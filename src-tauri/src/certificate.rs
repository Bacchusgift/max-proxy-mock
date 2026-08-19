use serde::Serialize;
use std::{fs, path::Path, process::Command};

const CERTIFICATE_NAME: &str = "Max Proxy Mock Rust CA";

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CertificateStatus {
    pub exists: bool,
    pub trusted: bool,
    pub path: String,
    pub message: String,
}

pub fn status(path: &Path) -> CertificateStatus {
    let display_path = path.to_string_lossy().into_owned();
    if !path.exists() {
        return CertificateStatus {
            exists: false,
            trusted: false,
            path: display_path,
            message: "本地根证书尚未生成，请重启应用".into(),
        };
    }
    let installed = installed_certificate_matches(path).unwrap_or(false);
    let verified = installed
        && Command::new("/usr/bin/security")
            .args(["verify-cert", "-c"])
            .arg(path)
            .args(["-p", "ssl", "-L"])
            .output()
            .map(|output| output.status.success())
            .unwrap_or(false);
    if verified {
        CertificateStatus {
            exists: true,
            trusted: true,
            path: display_path,
            message: "Max Proxy Mock 根证书已被 macOS 信任".into(),
        }
    } else {
        CertificateStatus {
            exists: true,
            trusted: false,
            path: display_path,
            message: if installed {
                "根证书已导入，但尚未获得 SSL 信任，HTTPS 请求仍会被拦截".into()
            } else {
                "根证书尚未写入当前用户钥匙串，Chrome 无法验证代理证书".into()
            },
        }
    }
}

pub fn install(path: &Path) -> Result<CertificateStatus, String> {
    if !path.exists() {
        return Err("本地根证书不存在，请重启应用后再试".into());
    }
    let keychain = default_user_keychain()?;
    let output = Command::new("/usr/bin/security")
        .args(["add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-k"])
        .arg(&keychain)
        .arg(path)
        .output()
        .map_err(|error| format!("无法启动 macOS 证书安装：{error}"))?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
        return Err(if !stderr.is_empty() {
            stderr
        } else if !stdout.is_empty() {
            stdout
        } else {
            "macOS 未接受证书信任设置".into()
        });
    }
    let result = status(path);
    if !result.trusted {
        return Err(result.message);
    }
    Ok(result)
}

fn default_user_keychain() -> Result<String, String> {
    let output = Command::new("/usr/bin/security")
        .args(["default-keychain", "-d", "user"])
        .output()
        .map_err(|error| format!("无法读取用户默认钥匙串：{error}"))?;
    if !output.status.success() {
        return Err("无法读取用户默认钥匙串".into());
    }
    let path = String::from_utf8_lossy(&output.stdout)
        .trim()
        .trim_matches('"')
        .to_string();
    if path.is_empty() {
        Err("当前用户没有可用的默认钥匙串".into())
    } else {
        Ok(path)
    }
}

fn installed_certificate_matches(path: &Path) -> Result<bool, String> {
    let local = fs::read_to_string(path).map_err(|error| error.to_string())?;
    let output = Command::new("/usr/bin/security")
        .args(["find-certificate", "-a", "-c", CERTIFICATE_NAME, "-p"])
        .output()
        .map_err(|error| format!("无法读取钥匙串证书：{error}"))?;
    if !output.status.success() {
        return Ok(false);
    }
    let installed = String::from_utf8_lossy(&output.stdout);
    let local_body = pem_body(&local);
    Ok(!local_body.is_empty()
        && pem_blocks(&installed)
            .iter()
            .any(|pem| pem_body(pem) == local_body))
}

fn pem_blocks(value: &str) -> Vec<&str> {
    value
        .split("-----END CERTIFICATE-----")
        .filter(|part| part.contains("-----BEGIN CERTIFICATE-----"))
        .collect()
}

fn pem_body(value: &str) -> String {
    value
        .lines()
        .filter(|line| !line.starts_with("-----"))
        .map(str::trim)
        .collect()
}
