"""Local Compose cart concurrency check: parallel additions must all survive.

Run from the repository root: python tests/integration/cart.py
Uses the real Gateway → Cart Service → Redis path with a Keycloak login; the cart is
emptied before and after so the demo data is unchanged.
"""
import concurrent.futures
import urllib.request
import uuid

from payments import API, STUDIO, login, request

PARALLEL_ADDS = 12


def empty_cart(token):
    headers = {"Authorization": "Bearer " + token, "X-Studio-Id": STUDIO}
    with urllib.request.urlopen(urllib.request.Request(API + "/v1/cart", None, headers, method="DELETE"), timeout=20) as response:
        assert response.status == 200, response.status


def main():
    student = login("student@bayareadancehub.local")
    products = request("/v1/credit-products", student)["products"]
    product = next(item for item in products if item.get("issuerScope") == "PLATFORM")
    version = product.get("versionId") or product.get("productVersionId") or product.get("id")
    empty_cart(student)
    try:
        # Every request reads the same cart; without compare-and-set the last writer wins
        # and quantities are lost. Each add carries its own idempotency key so none is
        # de-duplicated away.
        def add(_):
            return request("/v1/cart/items", student, {"productVersionId": version, "quantity": 1, "idempotencyKey": str(uuid.uuid4())})

        with concurrent.futures.ThreadPoolExecutor(max_workers=PARALLEL_ADDS) as pool:
            results = list(pool.map(add, range(PARALLEL_ADDS)))
        assert all(result.get("items") for result in results), results
        cart = request("/v1/cart", student)
        lines = [item for item in cart.get("items", []) if item["productVersionId"] == version]
        assert len(lines) == 1, cart
        assert int(lines[0]["quantity"]) == PARALLEL_ADDS, (cart, PARALLEL_ADDS)
    finally:
        empty_cart(student)
    print(f"PASS {PARALLEL_ADDS} parallel cart additions all kept their quantity")


if __name__ == "__main__":
    main()
