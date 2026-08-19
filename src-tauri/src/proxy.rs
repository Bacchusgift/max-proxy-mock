use crate::{
    core::Core,
    model::{domain_matches, Endpoint},
};
use chrono::Utc;
use http_body_util::BodyExt;
use hudsucker::{
    certificate_authority::RcgenAuthority,
    hyper::{header, HeaderMap, Request, Response, StatusCode},
    rcgen::{
        BasicConstraints, CertificateParams, DistinguishedName, DnType, IsCa, Issuer, KeyPair,
        KeyUsagePurpose,
    },
    rustls::crypto::ring,
    Body, HttpContext, HttpHandler, Proxy, RequestOrResponse,
};
use std::{
    collections::HashMap,
    fs,
    net::SocketAddr,
    path::{Path, PathBuf},
    sync::Arc,
    time::Instant,
};

const CAPTURE_LIMIT: usize = 2 * 1024 * 1024;

#[derive(Clone)]
pub struct CaptureHandler {
    core: Arc<Core>,
    pending: Option<Pending>,
}

#[derive(Clone)]
struct Pending {
    project_id: String,
    started: Instant,
    method: String,
    scheme: String,
    host: String,
    path: String,
    headers: HashMap<String, String>,
    body: String,
}

impl CaptureHandler {
    pub fn new(core: Arc<Core>) -> Self {
        Self {
            core,
            pending: None,
        }
    }
}

impl HttpHandler for CaptureHandler {
    async fn should_intercept_connect(
        &mut self,
        _ctx: &HttpContext,
        request: &Request<Body>,
    ) -> bool {
        request
            .uri()
            .host()
            .map(|host| self.core.should_mitm(host))
            .unwrap_or(false)
    }

    async fn should_intercept_tls(
        &mut self,
        _ctx: &HttpContext,
        hello: hudsucker::rustls::server::ClientHello<'_>,
    ) -> bool {
        hello
            .server_name()
            .map(|host| self.core.should_mitm(host))
            .unwrap_or(false)
    }

    async fn handle_request(
        &mut self,
        _ctx: &HttpContext,
        request: Request<Body>,
    ) -> RequestOrResponse {
        let host = request.uri().host().unwrap_or_default().to_lowercase();
        let path = request.uri().path().to_string();
        for rule in self.core.mocks() {
            if rule.enabled
                && domain_matches(&host, &rule.host)
                && rule.path == path
                && (rule.method.is_empty()
                    || rule.method.eq_ignore_ascii_case(request.method().as_str()))
            {
                let mut builder = Response::builder()
                    .status(StatusCode::from_u16(rule.status).unwrap_or(StatusCode::OK));
                for (name, value) in &rule.headers {
                    if !name.eq_ignore_ascii_case("content-length")
                        && !name.eq_ignore_ascii_case("content-encoding")
                    {
                        if let (Ok(name), Ok(value)) = (
                            header::HeaderName::try_from(name),
                            header::HeaderValue::try_from(value),
                        ) {
                            builder = builder.header(name, value);
                        }
                    }
                }
                if !rule
                    .headers
                    .keys()
                    .any(|key| key.eq_ignore_ascii_case("content-type"))
                {
                    builder =
                        builder.header(header::CONTENT_TYPE, "application/json; charset=utf-8");
                }
                return builder.body(Body::from(rule.body)).unwrap().into();
            }
        }

        let recording = self.core.recording();
        if !recording.active || !domain_matches(&host, &recording.domain) {
            return request.into();
        }
        let method = request.method().to_string();
        let scheme = request.uri().scheme_str().unwrap_or("https").to_string();
        let headers = headers_to_map(request.headers());
        let (parts, body) = request.into_parts();
        let bytes = match body.collect().await {
            Ok(body) => body.to_bytes(),
            Err(_) => return Request::from_parts(parts, Body::empty()).into(),
        };
        self.pending = Some(Pending {
            project_id: recording.project_id,
            started: Instant::now(),
            method,
            scheme,
            host,
            path,
            headers,
            body: preview(&bytes),
        });
        Request::from_parts(parts, Body::from(bytes)).into()
    }

    async fn handle_response(
        &mut self,
        _ctx: &HttpContext,
        response: Response<Body>,
    ) -> Response<Body> {
        let Some(pending) = self.pending.take() else {
            return response;
        };
        let status = response.status().as_u16();
        let headers = headers_to_map(response.headers());
        let content_type = response
            .headers()
            .get(header::CONTENT_TYPE)
            .and_then(|v| v.to_str().ok())
            .unwrap_or_default()
            .to_string();
        if content_type.to_lowercase().contains("text/event-stream") {
            return response;
        }
        let (parts, body) = response.into_parts();
        let bytes = match body.collect().await {
            Ok(body) => body.to_bytes(),
            Err(_) => return Response::from_parts(parts, Body::empty()),
        };
        let mut endpoint = Endpoint {
            id: String::new(),
            project_id: pending.project_id,
            method: pending.method,
            scheme: pending.scheme,
            host: pending.host,
            path: pending.path,
            status,
            request_headers: pending.headers,
            request_body: pending.body,
            response_headers: headers,
            response_body: preview(&bytes),
            content_type,
            duration_ms: pending.started.elapsed().as_millis() as i64,
            source: "recorded".into(),
            mocked: false,
            hit_count: 1,
            last_seen_at: Utc::now(),
        };
        if self.core.store.upsert_endpoint(&mut endpoint).is_ok() {
            self.core.emit("endpoints");
        }
        Response::from_parts(parts, Body::from(bytes))
    }
}

pub async fn start(core: Arc<Core>, certificate_dir: PathBuf) -> Result<(), String> {
    let (certificate, key) = ensure_ca(&certificate_dir)?;
    let key = KeyPair::from_pem(&key).map_err(|e| e.to_string())?;
    let issuer = Issuer::from_ca_cert_pem(&certificate, key).map_err(|e| e.to_string())?;
    let authority = RcgenAuthority::new(issuer, 1_000, ring::default_provider());
    let proxy = Proxy::builder()
        .with_addr(SocketAddr::from(([127, 0, 0, 1], 8899)))
        .with_ca(authority)
        .with_rustls_connector(ring::default_provider())
        .with_http_handler(CaptureHandler::new(core))
        .build()
        .map_err(|e| e.to_string())?;
    proxy.start().await.map_err(|e| e.to_string())
}

pub fn ensure_ca(directory: &Path) -> Result<(String, String), String> {
    fs::create_dir_all(directory).map_err(|e| e.to_string())?;
    let certificate_path = directory.join("max-proxy-ca.crt");
    let key_path = directory.join("max-proxy-ca.key");
    if certificate_path.exists() && key_path.exists() {
        return Ok((
            fs::read_to_string(certificate_path).map_err(|e| e.to_string())?,
            fs::read_to_string(key_path).map_err(|e| e.to_string())?,
        ));
    }
    let mut params = CertificateParams::new(Vec::<String>::new()).map_err(|e| e.to_string())?;
    let mut name = DistinguishedName::new();
    name.push(DnType::CommonName, "Max Proxy Mock Rust CA");
    params.distinguished_name = name;
    params.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
    params.key_usages = vec![
        KeyUsagePurpose::KeyCertSign,
        KeyUsagePurpose::DigitalSignature,
        KeyUsagePurpose::CrlSign,
    ];
    let key = KeyPair::generate().map_err(|e| e.to_string())?;
    let certificate = params.self_signed(&key).map_err(|e| e.to_string())?;
    let certificate_pem = certificate.pem();
    let key_pem = key.serialize_pem();
    fs::write(&certificate_path, &certificate_pem).map_err(|e| e.to_string())?;
    fs::write(&key_path, &key_pem).map_err(|e| e.to_string())?;
    Ok((certificate_pem, key_pem))
}

fn headers_to_map(headers: &HeaderMap) -> HashMap<String, String> {
    let mut result = HashMap::new();
    for (name, value) in headers {
        if let Ok(value) = value.to_str() {
            result.insert(name.as_str().to_string(), value.to_string());
        }
    }
    result
}
fn preview(bytes: &[u8]) -> String {
    let truncated = bytes.len() > CAPTURE_LIMIT;
    let bytes = &bytes[..bytes.len().min(CAPTURE_LIMIT)];
    let mut value = String::from_utf8_lossy(bytes).into_owned();
    if truncated {
        value.push_str("\n…（内容已截断）");
    }
    value
}
