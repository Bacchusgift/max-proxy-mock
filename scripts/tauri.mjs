import { execFileSync, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const args = process.argv.slice(2);

if (args[0] === "dev") {
  await prepareDevelopmentPorts();
}

const executable = path.join(root, "node_modules", ".bin", process.platform === "win32" ? "tauri.cmd" : "tauri");
const result = spawnSync(executable, args, { cwd: root, env: process.env, stdio: "inherit" });
process.exit(result.status ?? 1);

async function prepareDevelopmentPorts() {
  if (process.platform !== "darwin") return;

  const ports = [8899, 8900];
  const owners = new Map();
  for (const port of ports) {
    for (const pid of listeningPids(port)) owners.set(pid, port);
  }

  for (const [pid] of owners) {
    const command = processCommand(pid);
    if (isThisProject(command)) {
      console.log(`正在关闭旧的 Max Proxy Mock 实例（PID ${pid}）…`);
      try { process.kill(pid, "SIGTERM"); } catch {}
    }
  }

  for (let attempt = 0; attempt < 20; attempt++) {
    if (ports.every(port => listeningPids(port).length === 0)) return;
    await new Promise(resolve => setTimeout(resolve, 100));
  }

  const conflicts = ports.flatMap(port => listeningPids(port).map(pid => ({ port, pid, command: processCommand(pid) })));
  if (conflicts.length) {
    console.error("\n无法启动：本地代理端口被其他程序占用：");
    for (const item of conflicts) console.error(`- ${item.port}: PID ${item.pid} ${item.command}`);
    console.error("请关闭对应程序后重试。\n");
    process.exit(1);
  }
}

function listeningPids(port) {
  try {
    const output = execFileSync("lsof", ["-nP", `-iTCP:${port}`, "-sTCP:LISTEN", "-t"], { encoding: "utf8" });
    return [...new Set(output.trim().split(/\s+/).filter(Boolean).map(Number).filter(Number.isInteger))];
  } catch {
    return [];
  }
}

function processCommand(pid) {
  try { return execFileSync("ps", ["-p", String(pid), "-o", "command="], { encoding: "utf8" }).trim(); }
  catch { return ""; }
}

function isThisProject(command) {
  const normalizedRoot = root.replaceAll("\\", "/");
  const normalizedCommand = command.replaceAll("\\", "/");
  return normalizedCommand.includes(`${normalizedRoot}/src-tauri/target/`) ||
    normalizedCommand.includes(`${normalizedRoot}/src-tauri/target/debug/bundle/macos/Max Proxy Mock.app/`);
}
