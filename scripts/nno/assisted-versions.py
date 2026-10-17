#!/usr/bin/env python3
"""Return the version catalog from the lab server; keep authentication there."""

import contextlib
import io
import json
import sys
from urllib.request import Request, urlopen

from ailib import AssistedClient


if __name__ == '__main__':
    with contextlib.redirect_stdout(io.StringIO()):
        client = AssistedClient(quiet=True)
    if not client.token:
        raise RuntimeError('Assisted access token is unavailable')
    request = Request(sys.argv[1], headers={'Authorization': 'Bearer ' + client.token})
    with urlopen(request, timeout=60) as response:
        versions = json.load(response)
    print(json.dumps(versions))
