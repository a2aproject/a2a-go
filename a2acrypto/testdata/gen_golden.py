"""Generate the a2acrypto golden vector using the a2a-python reference SDK.

Produces testdata/golden.json: an AgentCard signed with a fixed Ed25519 key so
a2a-go can assert it reproduces the reference SDK's canonical bytes and
signature byte-for-byte. EdDSA signatures are deterministic (RFC 8032), so the
comparison is exact.
"""

import base64
import json

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from google.protobuf.json_format import MessageToDict

from a2a.utils import signing
from a2a import types

SEED = bytes(range(32))  # fixed 00 01 .. 1f for reproducibility
KID = "golden-ed25519-1"

priv = Ed25519PrivateKey.from_private_bytes(SEED)

# capabilities uses streaming=False and push_notifications=False so the golden
# exercises the explicit-false case.
card = types.AgentCard(
    name="Golden Agent",
    description="Cross-SDK golden vector agent.",
    version="1.0.0",
    provider=types.AgentProvider(organization="A2A", url="https://example.com"),
    capabilities=types.AgentCapabilities(streaming=False, push_notifications=False),
    default_input_modes=["text/plain"],
    default_output_modes=["text/plain"],
    supported_interfaces=[
        types.AgentInterface(
            url="https://example.com/a2a",
            protocol_binding="JSONRPC",
            protocol_version="0.3.0",
        )
    ],
    skills=[
        types.AgentSkill(
            id="s1", name="Skill One", description="does a thing", tags=["x", "y"]
        )
    ],
)
card_json_unsigned = MessageToDict(
    card
)  # capture before signing; the signer mutates in place

protected_header = {"alg": "EdDSA", "kid": KID, "typ": "JOSE"}
signer = signing.create_agent_card_signer(
    signing_key=priv, protected_header=protected_header
)
sig = signer(card).signatures[-1]

out = {
    "kid": KID,
    "ed25519_seed_hex": SEED.hex(),
    "card_json_unsigned": card_json_unsigned,
    "protected_b64": sig.protected,
    "signature_b64": sig.signature,
}
with open("golden.json", "w") as f:
    json.dump(out, f, indent=2, ensure_ascii=False)
print("wrote golden.json; signature:", sig.signature)
