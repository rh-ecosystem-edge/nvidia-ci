"""
Template utilities for loading HTML templates across workflows.
"""

import os
from typing import Optional


def load_template(filename: str, templates_dir: Optional[str] = None) -> str:
    """
    Load and return the contents of a template file.

    Args:
        filename: Name of the template file to load
        templates_dir: Optional path to templates directory. If not provided,
                      will look for templates/ relative to the caller's location

    Returns:
        The contents of the template file as a string

    Raises:
        FileNotFoundError: If the template file cannot be found
    """
    if templates_dir is None:
        # Default behavior: look for templates/ relative to caller's directory
        import inspect
        caller_file = inspect.stack()[1].filename
        caller_dir = os.path.dirname(os.path.abspath(caller_file))
        templates_dir = os.path.join(caller_dir, "templates")

    file_path = os.path.join(templates_dir, filename)

    if not os.path.exists(file_path):
        raise FileNotFoundError(f"Template file not found: {file_path}")

    with open(file_path, 'r', encoding='utf-8') as f:
        return f.read()