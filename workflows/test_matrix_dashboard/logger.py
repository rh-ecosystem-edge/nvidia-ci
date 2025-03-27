
import logging

logging.basicConfig(
    level=logging.DEBUG,  # Log all levels from DEBUG and above
    format="%(asctime)s [%(levelname)s] %(filename)s:%(lineno)d - %(message)s",  # Include time, level, filename, and line number
    handlers=[
        logging.StreamHandler(),  # Outputs to terminal
    ]
)

logger = logging.getLogger(__name__)
