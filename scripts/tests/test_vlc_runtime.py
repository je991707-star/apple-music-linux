"""Real ELF fixtures: python3 -m unittest discover -s scripts/tests -v."""
import importlib.util
import sys
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location('runtime', Path(__file__).parents[1] / 'vlc-runtime.py')
RUNTIME = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RUNTIME)


@unittest.skipUnless(shutil.which('gcc') and shutil.which('readelf') and shutil.which('ldd'), 'ELF tools required')
class RuntimeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.vlc = self.root / 'vlc'
        self.ff = self.root / 'ffmpeg'
        self.host = self.root / 'host'
        self.plug = self.vlc / 'plugins' / 'codec'
        for directory in [self.plug, self.ff, self.host]:
            directory.mkdir(parents=True)

    def lib(self, path, dependency=None, rpath=None, old_rpath=False, libc=False):
        source = self.root / 'source.c'
        text = 'int value(void) { return 1; }'
        if dependency:
            text = 'extern int value(void); int entry(void) { return value(); }'
        if libc:
            text = '#include <stdio.h>\nint entry(void) { return puts("test"); }'
        source.write_text(text)
        cmd = ['gcc', '-shared', '-fPIC', str(source), '-Wl,-soname,' + path.name, '-o', str(path)]
        if dependency:
            cmd += ['-Wl,--no-as-needed', str(dependency)]
        if rpath:
            cmd += ['-Wl,' + ('--disable-new-dtags,' if old_rpath else '') + '-rpath,' + str(rpath)]
        subprocess.run(cmd, check=True, capture_output=True)
        return path

    def audit(self, collect=False, ceiling='2.38'):
        RUNTIME.audit(self.vlc, self.ff, collect, ceiling)

    def test_transitive_evc_is_collected_and_dereferenced(self):
        evc = self.lib(self.host / 'libxevdb.so.0')
        actual = self.host / 'libxevdb.so.0.8'
        evc.rename(actual)
        evc.symlink_to(actual.name)
        ff = self.lib(self.ff / 'libavcodec.so.62', evc, self.host)
        self.lib(self.plug / 'libavcodec_plugin.so', ff)
        self.audit(True)
        self.assertFalse((self.vlc / evc.name).is_symlink())
        self.assertEqual((self.vlc / evc.name).read_bytes(), actual.read_bytes())
        self.assertTrue((self.vlc / 'licenses/xevd-COPYING').is_file())
        self.audit()

    def test_private_library_collection_source_is_not_a_runtime_search_path(self):
        dep = self.lib(self.host / 'libvlc_pulse.so.0')
        self.lib(self.plug / 'plugin.so', dep)
        RUNTIME.audit(self.vlc, self.ff, True, source_dirs=[self.host])
        self.audit()
        self.assertTrue((self.vlc / dep.name).exists())

    def test_xeve_dependency_shared_by_plugins_copies_once(self):
        dep = self.lib(self.host / 'libxeveb.so.0')
        self.lib(self.plug / 'first.so', dep, self.host)
        self.lib(self.plug / 'second.so', dep, self.host)
        self.audit(True)
        self.audit()
        self.assertTrue((self.vlc / dep.name).is_file())
        self.assertTrue((self.vlc / 'licenses/xeve-COPYING').is_file())
        self.assertEqual(len(list(self.vlc.glob('libxeve*'))), 1)

    def test_unneeded_evc_not_copied(self):
        self.lib(self.plug / 'plugin.so')
        self.audit(True)
        self.assertFalse((self.vlc / 'libxeveb.so.0').exists())

    def test_missing_transitive_dependency_fails(self):
        evc = self.lib(self.host / 'libxeveb.so.0')
        ff = self.lib(self.ff / 'libavcodec.so.62', evc, self.host)
        self.lib(self.plug / 'plugin.so', ff)
        evc.unlink()
        with self.assertRaisesRegex(RuntimeError, 'libxeveb.so.0'):
            self.audit()

    def test_verify_rejects_build_host_satisfying_evc(self):
        evc = self.lib(self.host / 'libxevdb.so.0')
        self.lib(self.plug / 'plugin.so', evc, self.host)
        with self.assertRaisesRegex(RuntimeError, 'outside package'):
            self.audit()

    def test_unknown_library_not_auto_copied(self):
        dep = self.lib(self.host / 'libunknown.so.1')
        self.lib(self.plug / 'plugin.so', dep, self.host)
        with self.assertRaisesRegex(RuntimeError, 'unreviewed external'):
            self.audit(True)
        self.assertFalse((self.vlc / dep.name).exists())

    def test_known_system_library_not_copied(self):
        self.lib(self.plug / 'plugin.so', libc=True)
        self.audit(True)
        self.assertFalse((self.vlc / 'libc.so.6').exists())

    def test_glibc_ceiling_enforced(self):
        self.lib(self.plug / 'plugin.so', libc=True)
        with self.assertRaisesRegex(RuntimeError, 'GLIBC_'):
            self.audit(ceiling='0.0')

    def test_core_library_rejected(self):
        self.lib(self.vlc / 'libc.so.6')
        with self.assertRaisesRegex(RuntimeError, 'core system library'):
            self.audit()

    def test_rpath_cannot_silently_override_bundle(self):
        evc = self.lib(self.host / 'libxevdb.so.0')
        shutil.copyfile(evc, self.vlc / evc.name)
        (self.vlc / 'licenses').mkdir()
        (self.vlc / 'licenses/xevd-COPYING').write_text('fixture')
        self.lib(self.plug / 'plugin.so', evc, self.host, old_rpath=True)
        with self.assertRaisesRegex(RuntimeError, 'loader uses'):
            self.audit(True)

    def test_optional_plugin_absent_is_ok(self):
        self.lib(self.vlc / 'libvlc.so.5')
        self.audit()

    def test_broken_symlink_fails(self):
        (self.vlc / 'libavcodec.so.62').symlink_to('missing')
        with self.assertRaisesRegex(RuntimeError, 'dangling'):
            self.audit()

    def test_symlink_cannot_escape_bundle(self):
        lib = self.lib(self.host / 'libxevdb.so.0')
        (self.vlc / lib.name).symlink_to(lib)
        with self.assertRaisesRegex(RuntimeError, 'escapes'):
            self.audit()

    def test_staged_artifact_cannot_borrow_from_source_bundle(self):
        evc = self.lib(self.vlc / 'libxeveb.so.0')
        self.lib(self.plug / 'plugin.so', evc, self.vlc)
        stage = self.root / 'stage'
        shutil.copytree(self.vlc, stage)
        (stage / evc.name).unlink()
        with self.assertRaisesRegex(RuntimeError, 'outside package'):
            RUNTIME.audit(stage, self.ff)

    def test_bundle_script_collects_then_revalidates_reused_bundle(self):
        repo = self.root / 'repo'
        scripts = repo / 'scripts'
        shutil.copytree(Path(__file__).parents[1], scripts,
                        ignore=shutil.ignore_patterns('__pycache__', 'tests'))
        libdir = self.root / 'system'
        src = libdir / 'vlc/plugins'
        libdir.mkdir()
        for name in ('libvlc.so.5.6.1', 'libvlccore.so.9.0.1'):
            self.lib(libdir / name)
        evc = self.lib(self.host / 'libxevdb.so.0')
        ff = repo / 'third_party/ffmpeg/lib'
        ff.mkdir(parents=True)
        codec = self.lib(ff / 'libavcodec.so.62', evc, self.host)
        for name in ('access/libimem_plugin.so', 'demux/libmp4_plugin.so',
                     'codec/libavcodec_plugin.so', 'demux/libavformat_plugin.so',
                     'audio_output/libalsa_plugin.so'):
            path = src / name
            path.parent.mkdir(parents=True, exist_ok=True)
            self.lib(path, codec if 'avcodec' in name else None)
        import os
        env = dict(os.environ, ADAM_VLC_LIBDIR=str(libdir))
        command = ['bash', str(scripts / 'bundle-vlc.sh')]
        result = subprocess.run(command, env=env, text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        dest = repo / 'electron/dist/resources/vlc'
        self.assertTrue((dest / 'libxevdb.so.0').is_file())
        self.assertFalse((dest / 'libxeveb.so.0').exists())
        result = subprocess.run(command, env=env, text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        (dest / 'plugins/codec/libavcodec_plugin.so').unlink()
        result = subprocess.run(command, env=env, text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('missing required VLC plugin', result.stderr)

    def three_level(self, external_rpath=False, private_name='libxevdb.so.0'):
        dep = self.lib(self.host / private_name)
        shutil.copyfile(dep, self.vlc / dep.name)
        (self.vlc / 'licenses').mkdir()
        (self.vlc / 'licenses/xevd-COPYING').write_text('fixture')
        middle = self.lib(self.vlc / 'libmiddle.so.1', dep)
        self.lib(self.plug / 'plugin.so', middle,
                 self.host if external_rpath else '$ORIGIN/../..', old_rpath=True)

    def test_ancestor_rpath_external_evc_rejected_in_both_modes(self):
        self.three_level(external_rpath=True)
        for collect in (False, True):
            with self.subTest(collect=collect):
                with self.assertRaisesRegex(RuntimeError, 'libxevdb.*loader uses|libxevdb.*outside package'):
                    self.audit(collect)

    def test_ancestor_rpath_external_private_library_rejected(self):
        self.three_level(external_rpath=True, private_name='libprivate.so.1')
        for collect in (False, True):
            with self.subTest(collect=collect):
                with self.assertRaisesRegex(RuntimeError, 'libprivate.*loader uses|libprivate.*outside package'):
                    self.audit(collect)

    def test_origin_rpath_three_level_bundle_and_system_dependencies_pass(self):
        self.three_level()
        for collect in (False, True):
            with self.subTest(collect=collect):
                self.audit(collect)
        self.assertFalse((self.vlc / 'libc.so.6').exists())

    def test_system_library_keeps_its_external_closure(self):
        private_system_dep = self.lib(self.host / 'libsystem_support.so.1')
        system = self.lib(self.host / 'libz.so.1', private_system_dep, '$ORIGIN')
        self.lib(self.plug / 'plugin.so', system, self.host)
        for collect in (False, True):
            with self.subTest(collect=collect):
                self.audit(collect)
        self.assertFalse((self.vlc / 'libz.so.1').exists())
        self.assertFalse((self.vlc / private_system_dep.name).exists())

    def test_direct_origin_runpath_preserves_file_symlink(self):
        actual = self.lib(self.vlc / 'libprivate.so.1.0')
        alias = self.vlc / 'libprivate.so.1'
        alias.symlink_to(actual.name)
        self.lib(self.plug / 'plugin.so', alias, '$ORIGIN/../..')
        for collect in (False, True):
            self.audit(collect)
        self.assertTrue(alias.is_symlink())

    def required_fixture(self):
        for name in ('libvlc.so.5', 'libvlccore.so.9'):
            self.lib(self.vlc / name)
        for name in ('access/libimem_plugin.so', 'demux/libmp4_plugin.so',
                     'codec/libavcodec_plugin.so', 'audio_output/libalsa_plugin.so'):
            path = self.vlc / 'plugins' / name
            path.parent.mkdir(parents=True, exist_ok=True)
            self.lib(path)

    def cli(self, collect=False):
        args = [sys.executable, str(Path(__file__).parents[1] / 'vlc-runtime.py')]
        if collect:
            args.append('--collect')
        return subprocess.run(args + [str(self.vlc), str(self.ff)],
                              capture_output=True, text=True)

    def test_cli_rejects_external_plugins_directory_in_both_modes(self):
        self.required_fixture()
        outside = self.host / 'plugins'
        (self.vlc / 'plugins').rename(outside)
        (self.vlc / 'plugins').symlink_to(outside, target_is_directory=True)
        for collect in (False, True):
            with self.subTest(collect=collect):
                result = self.cli(collect)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('directory symlink', result.stderr)
                self.assertIn(str(self.vlc / 'plugins'), result.stderr)

    def test_nested_plugin_directory_symlink_rejected_before_presence_check(self):
        outside = self.host / 'codec'
        self.plug.rename(outside)
        self.plug.symlink_to(outside, target_is_directory=True)
        result = self.cli()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('directory symlink', result.stderr)
        self.assertIn(str(self.plug), result.stderr)
        with self.assertRaisesRegex(RuntimeError, 'directory symlink'):
            self.audit()

    def test_internal_and_cyclic_directory_symlinks_are_rejected(self):
        # Conservative policy rejects internal directories too, without following loops.
        for target in (self.plug, self.vlc):
            link = self.vlc / 'loop'
            link.symlink_to(target, target_is_directory=True)
            with self.subTest(target=target):
                with self.assertRaisesRegex(RuntimeError, 'directory symlink'):
                    self.audit()
            link.unlink()

    def test_ffmpeg_directory_symlink_is_rejected(self):
        (self.ff / 'nested').symlink_to(self.host, target_is_directory=True)
        with self.assertRaisesRegex(RuntimeError, 'directory symlink'):
            self.audit()

    def test_cli_normal_tree_and_relative_library_symlink_pass(self):
        self.required_fixture()
        path = self.vlc / 'libvlc.so.5'
        path.rename(self.vlc / 'libvlc.so.5.0')
        path.symlink_to('libvlc.so.5.0')
        for collect in (False, True):
            with self.subTest(collect=collect):
                result = self.cli(collect)
                self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(path.is_symlink())


if __name__ == '__main__':
    unittest.main()
