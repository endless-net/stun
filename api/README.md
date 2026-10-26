# STUN: поддерживаемый стандартный wire profile

Producer — STUN, RB-stun. Требования — в
[единой спецификации workspace](../../../specs/001-endlessnet-project/spec.md).
Это контракт стандартного UDP Binding, без фиктивного Protobuf RPC/Go module.
Источник — [supported-protocol.md на 8628d35399ef9b23644e34a340291462493efb04](https://github.com/endless-net/stun/blob/8628d35399ef9b23644e34a340291462493efb04/docs/supported-protocol.md).

Запрос Binding `0x0001`, success response `0x0101`, magic cookie `0x2112A442`,
96-bit transaction ID. Ответ сохраняет transaction ID и возвращает
XOR-MAPPED-ADDRESS `0x0020` для наблюдаемого source IP/port. Address family
IPv4/IPv6 имеет стандартное encoding; codec vector не доказывает доступность
IPv6 сети/хоста. Синтаксис RFC8489 совместим с RFC5389 Binding wire subset.

Maximum datagram — 1500 bytes. Optional attributes игнорируются; malformed
header/length/cookie, unknown comprehension-required attributes, oversized и
unsupported messages silently dropped. Исторический minimal subset не имеет
`420 Unknown Attribute`, MESSAGE-INTEGRITY, credential authentication, RFC5780,
TCP/TLS STUN и TURN forwarding. Поэтому это не заявление полной RFC8489 реализации.
Public Binding не требует обязательного SPIFFE или Account database.

[vectors.json](vectors.json) содержит 9 offline сценариев: IPv4/IPv6 success,
optional attribute, short header, wrong cookie, inconsistent length, unsupported
method, unknown required attribute, oversized packet. TEST-NET адреса и
transaction ID — публичные fixtures, не реальные endpoints или credentials.

```bash
python3 -B api/test_wire_vectors.py
```

Команда выполняется из корня STUN; тест не запускает UDP server. Он проверяет
стандартный header, negative disposition и независимо декодирует XOR mapping.
Production rate limits, IPv6 host acceptance и client retry schedule требуют
отдельной реализации/приёмки. Host lifecycle, provisioning/deploy/activation
принадлежат отдельному Infrastructure и здесь не реализуются.
