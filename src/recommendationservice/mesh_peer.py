"""Bind asserted identity metadata to the mesh-proven workload identity.

Mirrors src/dancehub/internal/platform/mesh.go: the peer named by the SPIFFE URI in
x-forwarded-client-cert must be an allowed caller, and a ``service`` actor may only claim a
principal bound to that peer's ServiceAccount. Enforcement is off unless
MESH_PEER_ENFORCEMENT=true so Compose and the Kustomize development overlays keep working.

The module has no third-party imports on purpose: it is unit-tested in CI without grpcio.
"""

import re
from dataclasses import dataclass, field

SPIFFE_PATTERN = re.compile(r"^spiffe://([^/]+)/ns/([^/]+)/sa/([^/]+)$")

UNAUTHENTICATED = "UNAUTHENTICATED"
PERMISSION_DENIED = "PERMISSION_DENIED"


def parse_peers(value):
    """Parse ``sa,sa=principal|principal`` into {service_account: {principals}}."""
    peers = {}
    for raw_entry in (value or "").split(","):
        entry = raw_entry.strip()
        if not entry:
            continue
        account, _, principal_list = entry.partition("=")
        account = account.strip()
        if not account:
            raise ValueError(f'MESH_ALLOWED_PEERS entry "{entry}" has no service account')
        peers[account] = {principal.strip() for principal in principal_list.split("|") if principal.strip()}
    return peers


def peer_service_account(header):
    """Return (trust_domain, namespace, service_account) from the last XFCC element.

    Envoy appends the current client certificate as the last element, so earlier
    elements (which an upstream hop could have forged) are ignored.
    """
    value = (header or "").strip()
    if not value:
        return None
    last = value.split(",")[-1]
    for pair in last.split(";"):
        key, separator, uri = pair.partition("=")
        if not separator or key.strip().lower() != "uri":
            continue
        match = SPIFFE_PATTERN.match(uri.strip().strip('"'))
        if not match:
            return None
        return match.group(1), match.group(2), match.group(3)
    return None


@dataclass(frozen=True)
class MeshPolicy:
    enforce: bool = False
    trust_domain: str = "cluster.local"
    namespace: str = ""
    peers: dict = field(default_factory=dict)

    @classmethod
    def from_environment(cls, environment):
        if (environment.get("MESH_PEER_ENFORCEMENT") or "").strip().lower() != "true":
            return cls()
        namespace = (environment.get("MESH_NAMESPACE") or "").strip()
        if not namespace:
            raise ValueError("MESH_NAMESPACE is required when MESH_PEER_ENFORCEMENT is true")
        peers = parse_peers(environment.get("MESH_ALLOWED_PEERS"))
        if not peers:
            raise ValueError("MESH_ALLOWED_PEERS must list at least one caller when MESH_PEER_ENFORCEMENT is true")
        return cls(True, (environment.get("MESH_TRUST_DOMAIN") or "").strip() or "cluster.local", namespace, peers)

    def authorize(self, xfcc, actor_kind, service_principal):
        """Return None when allowed, otherwise (status_name, message)."""
        if not self.enforce:
            return None
        peer = peer_service_account(xfcc)
        if peer is None:
            return UNAUTHENTICATED, "mesh peer identity is required"
        trust_domain, namespace, account = peer
        if trust_domain != self.trust_domain or namespace != self.namespace:
            return PERMISSION_DENIED, "peer belongs to another trust domain or namespace"
        principals = self.peers.get(account)
        if principals is None:
            return PERMISSION_DENIED, "peer workload is not an allowed caller"
        if actor_kind == "service" and service_principal not in principals:
            return UNAUTHENTICATED, "service principal is not bound to the peer workload"
        return None
