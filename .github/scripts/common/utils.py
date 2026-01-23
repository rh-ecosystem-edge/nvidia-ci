import logging
from logging import Logger


def get_logger(name: str) -> Logger:
    logger = logging.getLogger(name)
    logger.setLevel(logging.INFO)
    if not logger.handlers:
        ch = logging.StreamHandler()
        ch.setLevel(logging.INFO)
        formatter = logging.Formatter('%(asctime)s [%(levelname)s] %(message)s')
        ch.setFormatter(formatter)
        logger.addHandler(ch)
    logger.propagate = False
    return logger


logger = get_logger(__name__)