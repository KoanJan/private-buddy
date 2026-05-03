# -*- mode: python ; coding: utf-8 -*-
"""
PyInstaller spec file for Private Buddy Python server.

Builds a standalone executable that bundles the FastAPI application
along with all dependencies including sqlite-vec native extension.
"""

import sys
import os
from pathlib import Path
from PyInstaller.utils.hooks import collect_submodules, collect_data_files

block_cipher = None

server_dir = SPECPATH

hidden_imports = [
    'uvicorn.logging',
    'uvicorn.loops',
    'uvicorn.loops.auto',
    'uvicorn.protocols',
    'uvicorn.protocols.http',
    'uvicorn.protocols.http.auto',
    'uvicorn.protocols.websockets',
    'uvicorn.protocols.websockets.auto',
    'uvicorn.lifespan',
    'uvicorn.lifespan.on',
    'sqlite_vec',
    'langchain_core',
    'langchain_openai',
    'langchain_community',
    'httpx',
    'websockets',
    'duckduckgo_search',
    'sqlalchemy.dialects.sqlite',
    'sqlalchemy.sql.default_comparator',
] + collect_submodules('app')

datas = []

binaries = []

sqlite_vec_ext = None
try:
    import sqlite_vec
    sqlite_vec_dir = os.path.dirname(sqlite_vec.__file__)
    for f in os.listdir(sqlite_vec_dir):
        if f.startswith('vec0') and (f.endswith('.so') or f.endswith('.dylib') or f.endswith('.dll')):
            sqlite_vec_ext = os.path.join(sqlite_vec_dir, f)
            break
    if sqlite_vec_ext:
        binaries.append((sqlite_vec_ext, 'sqlite_vec'))
except ImportError:
    pass

a = Analysis(
    [os.path.join(server_dir, 'app', 'main.py')],
    pathex=[server_dir],
    binaries=binaries,
    datas=datas,
    hiddenimports=hidden_imports,
    hookspath=[],
    hooksconfig={},
    runtime_hooks=[],
    excludes=[
        'tkinter',
        'matplotlib',
        'numpy.f2py',
        'scipy',
        'pandas',
        'IPython',
        'notebook',
        'pytest',
    ],
    noarchive=False,
    optimize=1,
    cipher=block_cipher,
)

pyz = PYZ(a.pure, cipher=block_cipher)

exe = EXE(
    pyz,
    a.scripts,
    [],
    exclude_binaries=True,
    name='private-buddy-server',
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=True,
    console=True,
    disable_windowed_traceback=False,
    argv_emulation=False,
    target_arch=None,
    codesign_identity=None,
    entitlements_file=None,
)

coll = COLLECT(
    exe,
    a.binaries,
    a.datas,
    strip=False,
    upx=True,
    upx_exclude=[],
    name='private-buddy-server',
)
