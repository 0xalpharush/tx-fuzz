# Independent dev chains: a compatibility test, not a mixed consensus network.
def run(plan):
    images = {
        "main": "ghcr.io/tempoxyz/tempo-localnet@sha256:1a8492c0b37474967df07480d919fbf03cedf34ea33a09818bba13a7b83fde84",
        "evm2": "ghcr.io/tempoxyz/tempo-localnet@sha256:cec6fea4262256d07ca622a33118207ca4fa417caf258882da8bea648f143fbb",
    }
    for name, image in images.items():
        plan.add_service(name=name, config=ServiceConfig(
            image=image,
            ports={"rpc": PortSpec(number=8545, application_protocol="http", wait="180s")},
            cmd=["--block-time", "200ms"],
            max_cpu=1000,
            max_memory=2048,
            ready_conditions=ReadyCondition(
                recipe=ExecRecipe(command=["/usr/local/bin/tempo-localnet", "--health"]),
                field="code", assertion="==", target_value=0,
                interval="1s", timeout="5m",
            ),
        ))
        plan.exec(service_name=name, recipe=ExecRecipe(command=["/usr/local/bin/tempo", "--version"]))
