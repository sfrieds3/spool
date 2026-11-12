import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

MODULE = Path(__file__).resolve().parents[1] / 'openspec_ci.py'
spec = importlib.util.spec_from_file_location('openspec_ci', MODULE)
ci = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ci)


class GitFixture(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.old = os.getcwd()
        os.chdir(self.tmp.name)
        subprocess.run(['git', 'init', '-q'], check=True)
        subprocess.run(['git', 'config', 'user.name', 'Test'], check=True)
        subprocess.run(['git', 'config', 'user.email', 'test@example.com'], check=True)
        Path('openspec/changes/example').mkdir(parents=True)
        Path('openspec/changes/archive/2025-01-02-old').mkdir(parents=True)
        self.base = self.commit('initial')
        subprocess.run(['git', 'update-ref', 'refs/remotes/origin/main', self.base], check=True)

    def tearDown(self):
        os.chdir(self.old)
        self.tmp.cleanup()

    def commit(self, message):
        subprocess.run(['git', 'commit', '-q', '--allow-empty', '-m', message], check=True)
        return subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()

    def test_true_footer_multiple_and_injection(self):
        self.assertEqual(ci.trailers('title\n\nOpenSpec-Change: example\nOpenSpec-Change: old\n', True), {'example', 'old'})
        for value in ['../escape', 'example;touch-bad', 'example other', 'EXAMPLE', 'example/evil']:
            with self.subTest(value=value), self.assertRaises(ValueError):
                ci.trailers('title\n\nOpenSpec-Change: ' + value, True)
        with self.assertRaises(ValueError):
            ci.trailers('title\n\nOpenSpec-Change: example\n\nNot a footer', True)
        with self.assertRaises(ValueError):
            ci.trailers('title\n\nOpenSpec-Change: none\nOpenSpec-Change: example', True)
        self.assertEqual(ci.trailers('title\n\nOpenSpec-Change: none', True), set())

    def test_range_and_merge_message(self):
        first = self.commit('initial')
        self.commit('first\n\nOpenSpec-Change: example')
        end = self.commit('second\n\nOpenSpec-Change: old')
        self.assertEqual(ci.commit_refs(first + '..' + end), {'example', 'old'})
        self.assertEqual(ci.trailers('Merge pull request #1 from branch'), set())

    def test_locations(self):
        self.assertEqual(ci.location('example'), 'active')
        self.assertEqual(ci.location('old'), 'archived')
        Path('openspec/changes/archive/2024-04-05-dated').mkdir()
        Path('openspec/changes/archive/2024-04-05-dated-extra').mkdir()
        self.assertEqual(ci.location('2024-04-05-dated'), 'archived')
        self.assertEqual(ci.location('dated'), 'archived')
        for name in ['missing', '../example', 'none', '2024-04-05-date', '2024-04-06-dated']:
            with self.subTest(name=name), self.assertRaises(ValueError):
                ci.location(name)

    def event(self, title='feat: work', body='OpenSpec-Change: example'):
        return {'repository': {'full_name': 'a/b'}, 'pull_request': {'title': title, 'body': body,
                'user': {'login': 'person'}, 'head': {'ref': 'topic', 'sha': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(), 'repo': {'full_name': 'a/b'}}}}

    def test_pr_contract_and_release_exemption(self):
        ci.pr_check(self.event())
        with self.assertRaises(ValueError):
            ci.pr_check(self.event(body='no footer'))
        with self.assertRaises(ValueError):
            ci.pr_check(self.event(body='OpenSpec-Change: missing'))
        event = self.event(body='')
        event['pull_request']['user']['login'] = 'github-actions[bot]'
        event['pull_request']['head']['ref'] = 'release-please--branches--main'
        ci.pr_check(event)
        event['pull_request']['head']['ref'] = 'release-please--branches--main--components--spool'
        ci.pr_check(event)
        event['pull_request']['head']['ref'] = 'release-please--branches--other'
        with self.assertRaises(ValueError):
            ci.pr_check(event)
        event['pull_request']['head']['ref'] = 'release-please--branches--main'
        event['pull_request']['head']['repo']['full_name'] = 'evil/fork'
        with self.assertRaises(ValueError):
            ci.pr_check(event)

    def test_archive_base_commits_and_rebase(self):
        event = self.event(body='OpenSpec-Change: none')
        event['pull_request']['head']['ref'] = 'automation/openspec-archive'
        with self.assertRaisesRegex(ValueError, 'lacks'):
            ci.pr_check(event)
        archive = self.commit(f'chore: archive\n\nOpenSpec-Change: none\nOpenSpec-Archive-Base: {self.base}')
        event['pull_request']['head']['sha'] = archive
        ci.pr_check(event)
        # Main advances while the original archive PR remains unchanged.
        subprocess.run(['git', 'checkout', '-q', '--detach', self.base], check=True)
        newer = self.commit('feat: newer main')
        subprocess.run(['git', 'update-ref', 'refs/remotes/origin/main', newer], check=True)
        with self.assertRaisesRegex(ValueError, 'regenerate'):
            ci.pr_check(event)
        # Replaying the old archive onto newer main cannot bless the stale marker.
        replay = self.commit(f'chore: archive\n\nOpenSpec-Archive-Base: {self.base}')
        event['pull_request']['head']['sha'] = replay
        with self.assertRaisesRegex(ValueError, 'regenerate'):
            ci.pr_check(event)
        # Even a forged updated marker must have the marked commit as its parent.
        subprocess.run(['git', 'checkout', '-q', '--detach', newer], check=True)
        middle = self.commit('extra commit')
        forged = self.commit(f'chore: archive\n\nOpenSpec-Archive-Base: {newer}')
        event['pull_request']['head']['sha'] = forged
        with self.assertRaisesRegex(ValueError, 'ancestry'):
            ci.pr_check(event)
        self.assertNotEqual(middle, newer)

    def test_archive_markers_any_branch_invalid_and_multiple(self):
        for message in ['OpenSpec-Archive-Base: bad', 'OpenSpec-Archive-Base: ' + self.base + '\n continuation',
                        'OpenSpec-Archive-Base: ' + self.base + '\nOpenSpec-Archive-Base: ' + self.base]:
            with self.subTest(message=message):
                subprocess.run(['git', 'checkout', '-q', '--detach', self.base], check=True)
                head = self.commit('chore: test\n\n' + message)
                event = self.event(body='OpenSpec-Change: none')
                event['pull_request']['head']['sha'] = head
                with self.assertRaises(ValueError):
                    ci.pr_check(event)

    def test_ready_and_incomplete(self):
        status = {'isComplete': True, 'artifacts': [{'status': 'done'}]}
        apply = {'state': 'all_done', 'progress': {'total': 1, 'complete': 1, 'remaining': 0}, 'tasks': [{'done': True}]}
        calls = []
        def fake_run(*args, **kwargs):
            calls.append(args)
            return ''
        with mock.patch.object(ci, 'json_cli', side_effect=[status, apply, {'archive': {'name': 'example'}, 'status': []}]) as cli, mock.patch.object(ci, 'run', side_effect=fake_run):
            ci.archive_one('example')
        self.assertEqual([c[1] for c in calls], ['validate'])
        self.assertEqual(cli.call_args.args, ('archive', 'example', '--yes'))
        with mock.patch.object(ci, 'json_cli', side_effect=[status, apply, {'archive': {'warnings': ['data loss']}}]), mock.patch.object(ci, 'run'):
            with self.assertRaises(RuntimeError):
                ci.archive_one('example')
        for patch in [ {'state': 'blocked'}, {'progress': {'total': 0, 'complete': 0, 'remaining': 0}}, {'tasks': [{'done': False}]} ]:
            with self.subTest(patch=patch), mock.patch.object(ci, 'json_cli', side_effect=[status, dict(apply, **patch)]), mock.patch.object(ci, 'run') as run:
                ci.archive_one('example')
                run.assert_not_called()
        with mock.patch.object(ci, 'run') as run:
            ci.archive_one('old')
            run.assert_not_called()

    def test_failures_do_not_publish_and_unknown_preflight(self):
        first = self.commit('initial')
        end = self.commit('feat: bad\n\nOpenSpec-Change: missing')
        with mock.patch.object(ci, 'archive_one') as archive, self.assertRaises(ValueError):
            ci.archive_push(first, end)
        archive.assert_not_called()
        status = {'isComplete': True, 'artifacts': [{'status': 'done'}]}
        apply = {'state': 'all_done', 'progress': {'total': 1, 'complete': 1, 'remaining': 0}, 'tasks': [{'done': True}]}
        with mock.patch.object(ci, 'json_cli', side_effect=[status, apply]), mock.patch.object(ci, 'run', side_effect=RuntimeError('validation error')) as run:
            with self.assertRaises(RuntimeError):
                ci.archive_one('example')
            self.assertEqual(run.call_count, 1)

    def test_multi_change_failure_propagates_without_final_validation(self):
        Path('openspec/changes/second').mkdir()
        end = self.commit('feat: both\n\nOpenSpec-Change: example\nOpenSpec-Change: second')
        real = ci.run
        with mock.patch.object(ci, 'archive_one', side_effect=[None, RuntimeError('archive failed')]) as archive, mock.patch.object(ci, 'run', wraps=real) as run:
            with self.assertRaisesRegex(RuntimeError, 'archive failed'):
                ci.archive_push(self.base, end)
        self.assertEqual(archive.call_count, 2)
        self.assertFalse(any(call.args[0] == 'openspec' for call in run.call_args_list))

    def test_recover_prior_range(self):
        first = self.commit('initial')
        middle = self.commit('feat: work\n\nOpenSpec-Change: example')
        end = self.commit('docs: unrelated\n\nOpenSpec-Change: none')
        real = ci.run
        with mock.patch.object(ci, 'archive_one') as archive, mock.patch.object(ci, 'run', wraps=real) as run:
            # Avoid needing the CLI for the final validation.
            def invoke(*args, **kwargs):
                return '' if args[0] == 'openspec' else real(*args, **kwargs)
            run.side_effect = invoke
            ci.archive_push(middle, end)
        archive.assert_called_once_with('example')


if __name__ == '__main__':
    unittest.main()
