"""Real ELF fixtures: python3 -m unittest discover -s scripts/tests -v."""
import importlib.util
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


if __name__ == '__main__':
    unittest.main()
