#!/usr/bin/env python3
import argparse
import json
import sys
import traceback

from jupyter_client import KernelManager


def emit(payload):
    sys.stdout.write(json.dumps(payload, ensure_ascii=True) + "\n")
    sys.stdout.flush()


def execute(client, request):
    cell_id = request.get("id", "")
    source = request.get("source", "")
    message_id = client.execute(source, allow_stdin=False, stop_on_error=False)
    outputs = []
    stdout = []
    stderr = []
    status = "ok"

    while True:
        message = client.get_iopub_msg(timeout=60)
        if message.get("parent_header", {}).get("msg_id") != message_id:
            continue
        message_type = message.get("msg_type")
        content = message.get("content", {})
        if message_type == "status" and content.get("execution_state") == "idle":
            break
        if message_type == "stream":
            text = content.get("text", "")
            name = content.get("name", "stdout")
            outputs.append({"output_type": "stream", "name": name, "text": text})
            (stderr if name == "stderr" else stdout).append(text)
        elif message_type in ("display_data", "execute_result"):
            output = {
                "output_type": message_type,
                "data": content.get("data", {}),
                "metadata": content.get("metadata", {}),
            }
            if message_type == "execute_result":
                output["execution_count"] = content.get("execution_count")
            outputs.append(output)
        elif message_type == "error":
            status = "error"
            trace = "\n".join(content.get("traceback", []))
            stderr.append(trace)
            outputs.append({
                "output_type": "error",
                "ename": content.get("ename", "Error"),
                "evalue": content.get("evalue", ""),
                "traceback": content.get("traceback", []),
            })

    shell_reply = client.get_shell_msg(timeout=30)
    if shell_reply.get("parent_header", {}).get("msg_id") == message_id:
        status = shell_reply.get("content", {}).get("status", status)
    return {
        "id": cell_id,
        "status": status,
        "stdout": "".join(stdout),
        "stderr": "".join(stderr),
        "outputs": outputs,
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--kernel", required=True)
    args = parser.parse_args()
    manager = KernelManager(kernel_name=args.kernel)
    manager.start_kernel()
    client = manager.blocking_client()
    client.start_channels()
    try:
        client.wait_for_ready(timeout=30)
        emit({"status": "ready", "kernel": args.kernel})
        for line in sys.stdin:
            try:
                request = json.loads(line)
                operation = request.get("op", "execute")
                if operation == "execute":
                    emit(execute(client, request))
                elif operation == "interrupt":
                    manager.interrupt_kernel()
                    emit({"id": request.get("id", ""), "status": "interrupted"})
                elif operation == "restart":
                    manager.restart_kernel(now=True)
                    client.wait_for_ready(timeout=30)
                    emit({"id": request.get("id", ""), "status": "restarted"})
                elif operation == "shutdown":
                    emit({"id": request.get("id", ""), "status": "shutdown"})
                    return
                else:
                    emit({"id": request.get("id", ""), "status": "error", "stderr": "unknown operation"})
            except Exception:
                emit({"id": "", "status": "error", "stderr": traceback.format_exc(), "outputs": []})
    finally:
        client.stop_channels()
        manager.shutdown_kernel(now=True)


if __name__ == "__main__":
    main()
