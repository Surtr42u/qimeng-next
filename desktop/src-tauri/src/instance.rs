use reqwest::{header, redirect, Client, StatusCode};
use serde::Deserialize;
use serde_json::{Map, Value};
use std::time::Duration;
use url::Url;

const SUPPORTED_API_VERSION: &str = "0.1.0";
const INSTANCE_LIMIT: usize = 64 * 1024;
const INVALID_INSTANCE: &str = "此地址未返回有效的影库实例信息，请检查地址后重试";

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Instance {
    id: String,
    #[serde(rename = "name")]
    _name: String,
    api_version: String,
    capabilities: Map<String, Value>,
}

fn check_instance(body: &[u8]) -> Result<(), &'static str> {
    let instance: Instance = serde_json::from_slice(body).map_err(|_| INVALID_INSTANCE)?;
    if instance.id.trim().is_empty() {
        return Err(INVALID_INSTANCE);
    }
    if instance.api_version != SUPPORTED_API_VERSION {
        return Err("此服务器的协议版本与客户端不兼容，请核对版本后重试");
    }
    let required = ["aiConfigured", "recommendations", "rankings", "stats", "backups", "migration"];
    if !required.iter().all(|key| instance.capabilities.get(*key).is_some_and(Value::is_boolean)) {
        return Err(INVALID_INSTANCE);
    }
    Ok(())
}

fn request_error(error: reqwest::Error) -> &'static str {
    if error.is_timeout() {
        "影库响应超时，请稍后重试"
    } else {
        "无法连接影库，请检查服务器地址和网络后重试"
    }
}

pub async fn verify(origin: &Url) -> Result<(), String> {
    let mut endpoint = origin.clone();
    endpoint.set_path("/api/v1/instance");
    // The native client never imports the active WebView's cookies or login credentials.
    let client = Client::builder()
        .tls_backend_native()
        .redirect(redirect::Policy::none())
        .timeout(Duration::from_secs(8))
        .build()
        .map_err(|_| "无法建立系统网络连接，请检查网络设置后重试")?;
    let mut response = client.get(endpoint)
        .header(header::ACCEPT, "application/json")
        .send().await.map_err(request_error)?;
    if response.status().is_redirection() {
        return Err("实例地址发生重定向，请填写最终服务器地址后重试".into());
    }
    if response.status() != StatusCode::OK {
        return Err(format!("服务器返回 HTTP {}，请检查影库地址后重试", response.status().as_u16()));
    }
    let media_type = response.headers().get(header::CONTENT_TYPE)
        .and_then(|header| header.to_str().ok())
        .and_then(|value| value.split(';').next());
    if !media_type.is_some_and(|value| value.trim().eq_ignore_ascii_case("application/json")) {
        return Err(INVALID_INSTANCE.into());
    }
    let mut body = Vec::new();
    while let Some(chunk) = response.chunk().await.map_err(request_error)? {
        if chunk.len() > INSTANCE_LIMIT - body.len() {
            return Err(INVALID_INSTANCE.into());
        }
        body.extend_from_slice(&chunk);
    }
    check_instance(&body).map_err(str::to_owned)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn checks_the_public_instance_contract_and_supported_version() {
        let mut info = json!({
            "id": "synthetic", "name": "Test library", "apiVersion": "0.1.0",
            "capabilities": {
                "aiConfigured": false, "recommendations": true, "rankings": true,
                "stats": true, "backups": true, "migration": true
            }
        });
        assert!(check_instance(info.to_string().as_bytes()).is_ok());
        info["id"] = json!("");
        assert!(check_instance(info.to_string().as_bytes()).is_err());
        info["id"] = json!("synthetic");
        info["apiVersion"] = json!("0.2.0");
        assert!(check_instance(info.to_string().as_bytes()).is_err());
        info["apiVersion"] = json!("0.1.0");
        info["capabilities"].as_object_mut().unwrap().remove("aiConfigured");
        assert!(check_instance(info.to_string().as_bytes()).is_err());
    }
}
