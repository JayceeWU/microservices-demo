import unittest

from mesh_peer import PERMISSION_DENIED, UNAUTHENTICATED, MeshPolicy, parse_peers, peer_service_account

GATEWAY_XFCC = (
    'By=spiffe://cluster.local/ns/dancehub/sa/recommendationservice;Hash=7d3f;Subject="";'
    "URI=spiffe://cluster.local/ns/dancehub/sa/gatewayservice"
)


def policy():
    return MeshPolicy.from_environment({
        "MESH_PEER_ENFORCEMENT": "true",
        "MESH_NAMESPACE": "dancehub",
        "MESH_ALLOWED_PEERS": "gatewayservice, orderservice=orderservice|order-refund-worker",
    })


class PeerServiceAccountTests(unittest.TestCase):
    def test_uses_only_the_last_certificate_element(self):
        forged = (
            "URI=spiffe://cluster.local/ns/dancehub/sa/gatewayservice,"
            "By=spiffe://cluster.local/ns/dancehub/sa/recommendationservice;Hash=1a2b;"
            "URI=spiffe://cluster.local/ns/dancehub/sa/orderservice"
        )
        self.assertEqual(peer_service_account(forged), ("cluster.local", "dancehub", "orderservice"))

    def test_rejects_non_workload_identities(self):
        for header in ["", "Hash=1a2b", "URI=https://example.com", "URI=spiffe://cluster.local/ns/x/deployment/y"]:
            self.assertIsNone(peer_service_account(header), header)


class MeshPolicyTests(unittest.TestCase):
    def test_binds_service_principals_to_peer(self):
        current = policy()
        cases = [
            ((GATEWAY_XFCC, "human", ""), None),
            ((GATEWAY_XFCC, "", ""), None),
            (("URI=spiffe://cluster.local/ns/dancehub/sa/orderservice", "service", "orderservice"), None),
            (("URI=spiffe://cluster.local/ns/dancehub/sa/orderservice", "service", "order-refund-worker"), None),
            (("", "human", ""), UNAUTHENTICATED),
            ((GATEWAY_XFCC, "service", "orderservice"), UNAUTHENTICATED),
            (("URI=spiffe://cluster.local/ns/dancehub/sa/chatservice", "human", ""), PERMISSION_DENIED),
            (("URI=spiffe://cluster.local/ns/staging/sa/gatewayservice", "human", ""), PERMISSION_DENIED),
            (("URI=spiffe://evil.example/ns/dancehub/sa/gatewayservice", "human", ""), PERMISSION_DENIED),
        ]
        for arguments, expected in cases:
            with self.subTest(arguments=arguments):
                result = current.authorize(*arguments)
                self.assertEqual(result[0] if result else None, expected)

    def test_inactive_unless_enforced(self):
        self.assertIsNone(MeshPolicy.from_environment({}).authorize("", "service", "anything"))

    def test_configuration_errors(self):
        with self.assertRaises(ValueError):
            MeshPolicy.from_environment({"MESH_PEER_ENFORCEMENT": "true", "MESH_NAMESPACE": "dancehub"})
        with self.assertRaises(ValueError):
            MeshPolicy.from_environment({"MESH_PEER_ENFORCEMENT": "true", "MESH_ALLOWED_PEERS": "gatewayservice"})
        with self.assertRaises(ValueError):
            parse_peers("gatewayservice,=worker")

    def test_default_trust_domain(self):
        self.assertEqual(policy().trust_domain, "cluster.local")


if __name__ == "__main__":
    unittest.main()
