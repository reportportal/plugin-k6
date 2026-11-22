#!/usr/bin/env python3
"""A stand-in for the ReportPortal API, to try the k6 output without a real server.

    python3 examples/mock-reportportal.py [port]        (default port 8080)

Run k6 with RP_ENDPOINT=http://127.0.0.1:<port>, any RP_PROJECT and any RP_API_KEY.
When the launch finishes, the mock prints the launch tree, the full log of the first failed
step under each request, and the launch description and attributes.
"""
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 8080

items = {}
launch = {"name": "", "status": "", "attributes": []}


def show_attributes(attributes):
    return ", ".join(f"{a.get('key', '')}={a['value']}" if a.get("key") else a["value"] for a in attributes)


def print_logs(logs, indent):
    for level, message in logs:
        for number, line in enumerate(message.splitlines() or [""]):
            print(f"{indent}{level + ':' if number == 0 else ' ' * (len(level) + 1)} {line}")


def print_tree(parent=None, depth=1):
    shown_failure = False
    for item_id, item in items.items():
        if item["parent"] != parent:
            continue
        indent = "  " * depth
        attributes = f"  ({show_attributes(item['attributes'])})" if item["attributes"] else ""
        print(f"{indent}{item['type']:<5} {item['name']}  [{item['status']}]{attributes}")
        is_failed_sample = item["type"] == "STEP" and parent is not None
        if is_failed_sample:
            if not shown_failure:
                print_logs(item["logs"], indent + "  ")
                shown_failure = True
        elif parent is None and item["type"] == "STEP":
            print_logs(item["logs"], indent + "  ")
        else:
            print_logs([(level, message.splitlines()[0]) for level, message in item["logs"]], indent + "  ")
        print_tree(item_id, depth + 1)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def send_json(self, code, payload):
        data = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def handle_request(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = json.loads(self.rfile.read(length)) if length else {}
        if not self.headers.get("Authorization", "").lower().startswith("bearer "):
            return self.send_json(401, {"message": "missing bearer token"})

        # /api/v1/<project>/<route>
        route = self.path.split("?")[0].strip("/").split("/")[3:]
        method = self.command

        if method == "POST" and route == ["launch"]:
            launch.update(name=body["name"], attributes=body.get("attributes", []))
            return self.send_json(201, {"id": "launch-1", "number": 1})
        if method == "POST" and route[:1] == ["item"]:
            item_id = f"item-{len(items) + 1}"
            items[item_id] = {
                "name": body["name"],
                "type": body["type"],
                "parent": route[1] if len(route) > 1 else None,
                "status": "",
                "attributes": [],
                "logs": [],
            }
            return self.send_json(201, {"id": item_id})
        if method == "PUT" and route[:1] == ["item"]:
            item = items[route[1]]
            item["status"] = body["status"]
            item["attributes"] = body.get("attributes", [])
            return self.send_json(200, {"message": "ok"})
        if method == "POST" and route == ["log"]:
            items[body["itemUuid"]]["logs"].append((body["level"], body["message"]))
            return self.send_json(201, {"id": "log"})
        if method == "PUT" and route[:1] == ["launch"] and route[-1] == "finish":
            launch["status"] = body["status"]
            print(f"\nLaunch: {launch['name']}  [{launch['status']}]")
            if launch["attributes"]:
                print(f"  attributes: {show_attributes(launch['attributes'])}")
            print_tree()
            return self.send_json(200, {"message": "ok"})
        if method == "GET" and route[:2] == ["launch", "uuid"]:
            return self.send_json(200, {"id": 1})
        if method == "PUT" and route[:1] == ["launch"] and route[-1] == "update":
            print(f"\nLaunch attributes: {show_attributes(body.get('attributes', []))}")
            print(f"Launch description:\n{body.get('description', '')}", flush=True)
            return self.send_json(200, {"message": "ok"})
        return self.send_json(404, {"message": "not handled by the mock"})

    do_GET = do_POST = do_PUT = handle_request


if __name__ == "__main__":
    print(f"Mock ReportPortal on http://127.0.0.1:{PORT}. Press Ctrl+C to stop.", flush=True)
    ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
