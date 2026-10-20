from pathlib import Path
import tempfile
import unittest
from results import reset_latest


class LatestResultsTests(unittest.TestCase):
    def test_replaces_latest_and_preserves_release_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            latest = reset_latest(root)
            (latest / 'old.log').write_text('old')
            release = root / 'test/results/ebyte-1.0.0-release'
            release.mkdir(); (release / 'evidence').write_text('keep')
            self.assertEqual(reset_latest(root), latest)
            self.assertEqual(list(latest.iterdir()), [])
            self.assertEqual((release / 'evidence').read_text(), 'keep')

    def test_rejects_symlink_at_every_managed_component(self):
        for relative in ('test', 'test/results', 'test/results/latest'):
            with self.subTest(relative=relative), tempfile.TemporaryDirectory() as directory:
                root = Path(directory) / 'repo'; root.mkdir()
                outside = Path(directory) / 'outside'; outside.mkdir()
                victim = outside / 'results/latest'; victim.mkdir(parents=True)
                (victim / 'keep').write_text('keep')
                link = root / relative; link.parent.mkdir(parents=True, exist_ok=True)
                link.symlink_to(outside, target_is_directory=True)
                with self.assertRaises(ValueError):
                    reset_latest(root)
                self.assertEqual((victim / 'keep').read_text(), 'keep')
                self.assertTrue(link.is_symlink())

    def test_rejects_files_without_removal(self):
        for relative in ('test', 'test/results', 'test/results/latest'):
            with self.subTest(relative=relative), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                path = root / relative; path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('keep')
                with self.assertRaises(ValueError):
                    reset_latest(root)
                self.assertEqual(path.read_text(), 'keep')

    def test_does_not_follow_links_inside_latest(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / 'repo'; root.mkdir()
            outside = Path(directory) / 'outside'; outside.mkdir()
            (outside / 'keep').write_text('keep')
            latest = reset_latest(root)
            (latest / 'link').symlink_to(outside, target_is_directory=True)
            reset_latest(root)
            self.assertEqual((outside / 'keep').read_text(), 'keep')
