"""The --slots flag the suites that keep requests in flight take: agent_quality.py's, vision_quality.py's
and tau2_quality.py's eval, and mteb_quality.py. It is the model's slots, from its host-class
profile, which evaluate (package evaluation/gate, Runner.slotsOf) passes each suite, so a suite keeps
as many requests in flight on the model as the router gives it slots, and the profile is the one
source of the count. Its test is test_slots.py.
"""

import argparse


def positive(text: str) -> int:
    """A slot count: a whole number of at least one."""
    try:
        n = int(text)
    except ValueError:
        raise argparse.ArgumentTypeError(f"{text!r} is not a whole number") from None
    if n < 1:
        raise argparse.ArgumentTypeError(f"{n} slots: at least one")
    return n


def add_slots(parser: argparse.ArgumentParser) -> None:
    """Adds the required --slots flag to parser."""
    parser.add_argument("--slots", type=positive, required=True,
                        help="the model's slots, from its profile: the requests kept in flight on it")
