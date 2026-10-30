"""Backend selection for CLI end-to-end tests."""

import os
import unittest


def ay_only(reason):
    """Mark a non-graph test or class that upstream ya must not execute."""
    return unittest.skipIf(os.environ.get("AY_E2E_BACKEND") == "ya", "ay only: " + reason)
