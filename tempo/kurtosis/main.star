# One Tempo chain: revm produces blocks and EVM2's debug consensus client
# imports and executes the exact same canonical payloads. Generators submit
# only to the producer.
def run(plan, args):
    # Tempo's checked-in dev genesis starts at timestamp zero. Expiring nonces
    # use Unix time, so generate one shared current-time genesis for both ELs.
    genesis_source = plan.upload_files(
        src="./dev-genesis",
        name="tempo-dev-genesis-source",
    )
    genesis = plan.run_python(
        run="""
import json
import os
import time

with open('/input/dev.json') as source:
    spec = json.load(source)
spec['timestamp'] = hex(int(time.time()) - 2)
os.makedirs('/output', exist_ok=True)
with open('/output/dev.json', 'w') as output:
    json.dump(spec, output, separators=(',', ':'))
""",
        files={"/input": genesis_source},
        store=[StoreSpec(src="/output", name="tempo-dev-genesis")],
    )
    genesis_files = genesis.files_artifacts[0]

    plan.print(
        "tempo-revm source={} engine={} image={}".format(
            args["baseline_source"], args["baseline_engine"], args["baseline_image"]
        )
    )
    plan.add_service(
        name="tempo-revm",
        config=ServiceConfig(
            image=args["baseline_image"],
            entrypoint=["/usr/local/bin/tempo"],
            ports={
                "rpc": PortSpec(number=8545, application_protocol="http", wait="180s"),
                "ws": PortSpec(number=8546, application_protocol="http"),
                "p2p": PortSpec(number=30303, application_protocol="tcp"),
            },
            cmd=[
                "node",
                "--dev",
                "--addr",
                "0.0.0.0",
                "--chain",
                "/config/dev.json",
                "--dev.block-time",
                "{}".format(args["block_time"]),
                "--datadir",
                "/data",
                "--tempo.bootnodes-endpoint",
                "none",
                "--disable-discovery",
                "--no-persist-peers",
                "--http",
                "--http.addr",
                "0.0.0.0",
                "--http.port",
                "8545",
                "--http.api",
                "all",
                "--http.corsdomain",
                "*",
                "--ws",
                "--ws.addr",
                "0.0.0.0",
                "--ws.port",
                "8546",
                "--ws.api",
                "all",
                "--builder.gaslimit",
                "3000000000",
                "--faucet.enabled",
                "--faucet.private-key",
                "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
                "--faucet.amount",
                "1000000000000000",
                "--faucet.node-address",
                "http://127.0.0.1:8545",
                "--faucet.address",
                "0x20c0000000000000000000000000000000000000",
                "0x20c0000000000000000000000000000000000001",
                "0x20c0000000000000000000000000000000000002",
                "0x20c0000000000000000000000000000000000003",
            ],
            files={"/config": genesis_files},
            max_memory=3072,
        ),
    )

    plan.print(
        "tempo-evm2 source={} engine={} image={}".format(
            args["candidate_source"], args["candidate_engine"], args["candidate_image"]
        )
    )
    plan.add_service(
        name="tempo-evm2",
        config=ServiceConfig(
            image=args["candidate_image"],
            entrypoint=["/usr/local/bin/tempo"],
            cmd=[
                "node",
                "--addr",
                "0.0.0.0",
                "--chain",
                "/config/dev.json",
                "--follow",
                "ws://tempo-revm:8546",
                "--follow.nocertify",
                "--datadir",
                "/data",
                "--tempo.bootnodes-endpoint",
                "none",
                "--disable-discovery",
                "--no-persist-peers",
                "--http",
                "--http.addr",
                "0.0.0.0",
                "--http.port",
                "8545",
                "--http.api",
                "all",
                "--http.corsdomain",
                "*",
                "--builder.gaslimit",
                "3000000000",
            ],
            ports={
                "rpc": PortSpec(number=8545, application_protocol="http", wait="180s"),
                "p2p": PortSpec(number=30303, application_protocol="tcp"),
            },
            files={"/config": genesis_files},
            max_memory=3072,
        ),
    )

    # Fund txgen's deterministic Anvil accounts before either generator starts.
    plan.add_service(
        name="tempo-chaos",
        config=ServiceConfig(
            image=args["spammer_image"],
            cmd=[],
            min_memory=64,
            max_memory=1024,
        ),
    )
    plan.exec(
        service_name="tempo-chaos",
        recipe=ExecRecipe(
            command=[
                "/usr/local/bin/tempo-fund",
                "--rpc=http://tempo-revm:8545",
                "--addresses=f39fd6e51aad88f6f4ce6ab8827279cfffb92266,70997970c51812dc3a010c7d01b50e0d17dc79c8,3c44cdddb6a900fa2b585dd299e03d12fa4293bc,90f79bf6eb2c4f870365e785982e1f101e93b906,15d34aaf54267db7d7c367839aaf71a00a2c6a65,9965507d1a55bcc2695c58ba16fb37d819b0a4dc,976ea74026e726554db657fa54763abd0c3a0aa9,14dc79964da2c08b23698b3d3cc7ca32193d9955,23618e81e3f5cdf7f54c3d65f7fb721e1f428abd,a0ee7a142d267c1f36714e4a8f75612f20a79720",
                "--timeout=120s",
            ]
        ),
    )

    # Upstream forkmon remains a live dashboard. The command-line oracle below
    # is the strict pass/fail gate and additionally compares vmTrace responses.
    forkmon_files = plan.upload_files(
        src="./el-forkmon-config",
        name="tempo-el-forkmon-config",
    )
    plan.add_service(
        name="el-forkmon",
        config=ServiceConfig(
            image=args["forkmon_image"],
            files={"/config": forkmon_files},
            cmd=["/config/config.toml"],
            ports={"http": PortSpec(number=8080, application_protocol="http")},
            max_memory=256,
        ),
    )

    # txgen continuously provides structured Tempo, parallel-nonce, expiring-
    # nonce, and EIP-1559 traffic for the full soak.
    plan.add_service(
        name="txgen",
        config=ServiceConfig(
            image=args["txgen_image"],
            entrypoint=["/bin/sh", "-c"],
            cmd=[
                "txgen-tempo generate --defer-signing --spec /specs/tempo-bench/presets/mix.yml "
                + "--duration {} --seed {} --rpc http://tempo-revm:8545 | ".format(
                    args["duration"], args["seed"]
                )
                + "bench send --rpc-url http://tempo-revm:8545 --tps {} ".format(
                    args["txgen_tps"]
                )
                + "--late-signing-spec /specs/tempo-bench/presets/mix.yml "
                + "--max-concurrent 64 --retries 3 --report console",
            ],
            env_vars={
                "TXGEN_ACCOUNTS": "10",
                "TXGEN_TIP20_TOKENS": '["0x20c0000000000000000000000000000000000000","0x20c0000000000000000000000000000000000001","0x20c0000000000000000000000000000000000002","0x20c0000000000000000000000000000000000003"]',
            },
            max_memory=1024,
        ),
    )

    plan.add_service(
        name="tx-fuzz",
        config=ServiceConfig(
            image=args["spammer_image"],
            entrypoint=["/usr/local/bin/tempo-evm-diff"],
            cmd=[
                "--single-rpc=http://tempo-revm:8545",
                "--seed={}".format(args["seed"]),
                "--programs={}".format(args["programs_per_seed"]),
                "--max-code-bytes={}".format(args["max_code_bytes"]),
                "--timeout={}".format(args["duration"]),
            ],
            min_memory=64,
            max_memory=1024,
        ),
    )
    # The candidate's RPC consensus client imports live canonical payloads.
    # Connect the peers so normal reth sync can backfill any blocks mined before
    # the subscription was ready, then compare both independently executed views.
    plan.exec(
        service_name="tempo-chaos",
        recipe=ExecRecipe(
            command=[
                "/usr/local/bin/evm-rpc-oracle",
                "--left-rpc=http://tempo-revm:8545",
                "--right-rpc=http://tempo-evm2:8545",
                "--chain-id=1337",
                "--connect-peers",
                "--duration={}".format(args["oracle_duration"]),
                "--min-blocks={}".format(args["min_blocks"]),
                "--max-lag={}".format(args["max_oracle_lag"]),
                "--stall-timeout={}".format(args["stall_timeout"]),
            ]
        ),
    )
