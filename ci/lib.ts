import Timeout from "await-timeout"
import cp from "child-process-promise"
import { ExecOptions, SpawnOptions } from "child_process"

export async function main(fn: (ctx: Promise<unknown>) => void, opts: {
  timeout: number
} = {
  timeout: 3 * 60_000,
}) {

  const timer = new Timeout();
  let ctx: Promise<unknown> = timer.set(opts.timeout, "context timed out")

  const interrupted = new Promise((res, rej) => {
    let int = 0
    process.on("SIGINT", () => {
      int++
      if (int === 2) {
        console.log("force exited")
        process.exit(1)
      }
      rej("")
    })
  })

  ctx = Promise.race([ctx, interrupted])
  const {res, rej, p} = withCancel(ctx)
  ctx = p

  try {
    await init(ctx)
    await fn(ctx)
    res!()
  } catch (e) {
    console.log(e)
    rej!()
    process.on("beforeExit", () => {
      process.exit(1)
    })
  } finally {
    timer.clear()
  }
}

// TODO promisify native versions
export async function exec(ctx: Promise<unknown>, cmd: string, opts?: ExecOptions) {
  opts = {
    ...opts,
  }
  const p = cp.exec(cmd, opts)

  try {
    return await selectCtx(ctx, p)
  } finally {
    p.childProcess.kill()
  }
}

export async function spawn(ctx: Promise<unknown>, cmd: string, args: string[], opts?: SpawnOptions) {
  if (args === undefined) {
    args = []
  }
  opts = {
    shell: true,
    ...opts,
  }
  const p = cp.spawn(cmd, args, opts)

  try {
    return await selectCtx(ctx, p)
  } finally {
    p.childProcess.kill()
  }
}

async function init(ctx: Promise<unknown>) {
  const r = await exec(ctx, "git rev-parse --show-toplevel", {
    cwd: __dirname,
  })

  process.chdir(r.stdout.toString().trim())
}

export async function selectCtx<T>(ctx: Promise<unknown>, p: Promise<T>): Promise<T> {
  return await Promise.race([ctx, p]) as Promise<T>
}

const cancelSymbol = Symbol()

export function withCancel<T>(p: Promise<T>) {
  let rej: () => void;
  let res: () => void;
  const p2 = new Promise<T>((res2, rej2) => {
    res = res2
    rej = () => {
      rej2(cancelSymbol)
    }
  })

  p = Promise.race<T>([p, p2])
  p = p.catch(e => {
    // We need this catch to prevent node from complaining about it being unhandled.
    // Look into why more later.
    if (e === cancelSymbol) {
      return
    }
    throw e
  }) as Promise<T>

  return {
    res: res!,
    rej: rej!,
    p: p,
  }
}


export const wasmEnv = {
  ...process.env,
  GOOS: "js",
  GOARCH: "wasm",
}
