"""Manage only the trusted repository's fixed latest result directory."""
from pathlib import Path
import shutil


def reset_latest(root):
    root = Path(root)
    # The checkout and its parent are trusted against concurrent writers.
    # Check every managed component, not just the final directory.
    for relative in ('test', 'test/results', 'test/results/latest'):
        path = root / relative
        if path.is_symlink() or (path.exists() and not path.is_dir()):
            raise ValueError('result paths must be real managed directories')
    latest = root / 'test/results/latest'
    if latest.exists():
        shutil.rmtree(latest)
    latest.mkdir(parents=True)
    return latest


if __name__ == '__main__':
    reset_latest(Path(__file__).resolve().parents[1])
