# Independent dev chains: a compatibility test, not a mixed consensus network.
def run(plan, args):
    images = {
        "baseline": args["baseline_image"],
        "candidate": args["candidate_image"],
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
                interval="1s", timeout="150s",
            ),
        ))
        plan.exec(service_name=name, recipe=ExecRecipe(command=["/usr/local/bin/tempo", "--version"]))
