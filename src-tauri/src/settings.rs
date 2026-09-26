use std::{collections::HashSet, net::IpAddr};

pub fn validate_dns_request(
    dns: Option<String>,
    dns_servers: Option<Vec<String>>,
    dns_doh: Option<String>,
) -> Result<(String, Vec<String>, String), String> {
    let dns = dns.unwrap_or_else(|| "cloudflare".into());
    if ["cloudflare", "google", "quad9"].contains(&dns.as_str()) {
        return Ok((dns, Vec::new(), String::new()));
    }
    if dns == "subscription-doh" {
        let value = dns_doh.unwrap_or_default();
        if !valid_https_endpoint(&value, false) {
            return Err("Подписка не предоставила корректный DoH-адрес".into());
        }
        return Ok((dns, Vec::new(), value));
    }
    if dns != "custom" {
        return Err("Выбран неизвестный DNS-сервер".into());
    }
    let values = dns_servers.unwrap_or_default();
    if values.is_empty() || values.len() > 4 {
        return Err("Укажите от одного до четырёх DNS IP-адресов".into());
    }
    let mut unique = HashSet::with_capacity(values.len());
    let mut servers = Vec::with_capacity(values.len());
    for value in values {
        if value.len() > 45 {
            return Err("Пользовательский DNS должен содержать только IPv4 или IPv6-адреса".into());
        }
        let address: IpAddr = value
            .trim()
            .parse()
            .map_err(|_| "Пользовательский DNS должен содержать только IPv4 или IPv6-адреса")?;
        let broadcast = matches!(address, IpAddr::V4(ip) if ip.octets() == [255, 255, 255, 255]);
        if address.is_unspecified() || address.is_multicast() || broadcast {
            return Err("Этот DNS IP-адрес нельзя использовать".into());
        }
        if unique.insert(address) {
            servers.push(address.to_string());
        }
    }
    Ok((dns, servers, String::new()))
}

pub fn valid_https_endpoint(value: &str, allow_query: bool) -> bool {
    if value.is_empty()
        || value.len() > 2048
        || value
            .chars()
            .any(|ch| ch.is_control() || ch == ' ' || ch == '#')
        || (!allow_query && value.contains('?'))
    {
        return false;
    }
    let Some(rest) = value.strip_prefix("https://") else {
        return false;
    };
    let authority = rest.split('/').next().unwrap_or_default();
    !authority.is_empty() && !authority.contains('@')
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_built_in_and_custom_dns() {
        assert_eq!(
            validate_dns_request(None, None, None).unwrap(),
            ("cloudflare".into(), vec![], String::new())
        );
        assert_eq!(
            validate_dns_request(
                Some("custom".into()),
                Some(vec![" 192.168.1.1 ".into(), "192.168.1.1".into()]),
                None,
            )
            .unwrap(),
            ("custom".into(), vec!["192.168.1.1".into()], String::new())
        );
        assert_eq!(
            validate_dns_request(
                Some("subscription-doh".into()),
                None,
                Some("https://dns.example/dns-query".into())
            )
            .unwrap(),
            (
                "subscription-doh".into(),
                vec![],
                "https://dns.example/dns-query".into()
            )
        );
    }

    #[test]
    fn rejects_invalid_custom_dns() {
        for values in [
            vec![],
            vec!["not-an-ip".into()],
            vec!["0.0.0.0".into()],
            vec!["::".into()],
            vec!["224.0.0.1".into()],
        ] {
            assert!(validate_dns_request(Some("custom".into()), Some(values), None).is_err());
        }
        assert!(validate_dns_request(
            Some("subscription-doh".into()),
            None,
            Some("http://dns.example".into())
        )
        .is_err());
        assert!(valid_https_endpoint(
            "https://t.me/support?start=client",
            true
        ));
    }
}
