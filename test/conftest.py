import logging
import os
import shutil
import sys
from functools import partial

import pytest
import sh


class PSQL(object):
    # A helper object to do SQL queries with real psql.
    def __init__(self):
        from sh import psql
        self.psql = psql.bake("--no-psqlrc")

    def __call__(self, *a, **kw):
        return self.psql(*a, **kw)

    def scalar(self, select, *a, **kw):
        return next(iter(self.select1(select, *a, **kw)))

    def select1(self, select, *a, **kw):
        # Execute a SELECT and yield each line as a single value.
        return filter(None, (
            line.strip()
            for line in self('-tc', select, *a, _iter=True, **kw)
        ))

    def members(self, role):
        # List members of role
        return self.select1(
            # Good old SQL injection. Who cares?
            u"SELECT DISTINCT m.rolname FROM pg_roles AS m "
            u"JOIN pg_auth_members a ON a.member = m.oid "
            u"JOIN pg_roles AS r ON r.oid = a.roleid "
            u" WHERE r.rolname = '%s' "
            u"ORDER BY 1;" % (role,)
        )

    def roles(self):
        # List **all** roles
        return self.select1("SELECT rolname FROM pg_roles;")

    def superusers(self):
        # List superusers
        return self.select1(
            "SELECT rolname FROM pg_roles WHERE rolsuper IS TRUE;"
        )

    def tables(self, *a, **kw):
        # List tables
        return self.select1(
            u"SELECT relname "
            u"FROM pg_catalog.pg_class c "
            u"JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace "
            u"WHERE "
            u"    c.relkind = 'r' "
            u"    AND n.nspname !~ '^pg_' "
            u"    AND n.nspname <> 'information_schema' "
            u"ORDER BY 1;",
            *a, **kw
        )

    def config(self, role):
        """ Get a dictionary of configuration settings for a role. """
        config_lines = self.select1(u"""
            SELECT unnest(rolconfig)
              FROM pg_roles
             WHERE rolname='{rolname}'
        """.format(rolname=role,))
        c = {}
        for config_line in config_lines:
            kv = config_line.split('=')
            c[kv[0]] = kv[1]
        return c


@pytest.fixture(scope='module', autouse=True)
def pgenv(request):
    mod = request.module.__name__.replace('test_', '')
    if 'extra' == mod:
        os.environ['PGUSER'] = 'postgres'
        os.environ['PGDATABASE'] = 'extra0'
    else:
        os.environ['PGUSER'] = 'ldap2pg'
        os.environ['PGDATABASE'] = 'nominal'


@pytest.fixture(scope='module')
def psql():
    # Supply the PSQL helper as a pytest fixture.
    return PSQL()


class LDAP(object):
    # Helper to query LDAP with creds from envvars.
    def __init__(self):
        self.common_args = (
            '-xv',
            '-w', os.environ['LDAPPASSWORD'],
        )

        self.search = sh.ldapsearch.bake(*self.common_args)

    def search_sub_dn(self, base):
        # Iter dn under base entry, excluded.
        for line in self.search('-b', base, 'dn', _iter=True):
            if not line.startswith('dn: '):
                continue

            if line.startswith('dn: ' + base):
                continue

            yield line.strip()[len('dn: '):]


@pytest.fixture(scope='session')
def ldap():
    # Supply LDAP helper as a pytest fixture
    #
    # def test_rockon(ldap):
    #     entries = ldap.search(...)
    return LDAP()


@pytest.fixture(scope='module', autouse=True)
def resetpostgres(sh_errout):
    from sh import Command

    Command('test/fixtures/postgres/reset.sh')()
    Command('test/fixtures/postgres/nominal.sh')()
    Command('test/fixtures/postgres/extra.sh')()


def lazy_write(attr, data):
    # Lazy access sys.{stderr,stdout} to mix with capsys.
    getattr(sys, attr).write(data.replace(r"\n", "\n"))
    return False  # should_quit


@pytest.fixture(scope='session', autouse=True)
def sh_errout():
    logging.getLogger('sh').setLevel(logging.ERROR)

    # Duplicate tested command stdio to pytest capsys.
    sh._SelfWrapper__self_module.Command._call_args.update(dict(
        err=partial(lazy_write, 'stderr'),
        out=partial(lazy_write, 'stdout'),
        tee=True,
    ))


@pytest.fixture(scope='session', autouse=True)
def dot_env():
    # test dot env file loading
    # moving LDAPPASSWORD from environement to .env file
    ldappasswd = os.environ['LDAPPASSWORD']
    if os.path.exists(".env") or os.path.exists("test/.env"):
        pytest.fail(".env or test/.env already exists")
    del os.environ['LDAPPASSWORD']
    with open(".env", "w") as f:
        f.write("LDAPPASSWORD=%s\n" % ldappasswd)
    shutil.copy(".env", "test/.env")
    yield
    os.unlink(".env")
    os.unlink("test/.env")


def loggername_factory(ran, call_args, pid=None):
    sys.stderr.write("+ %s\n" % (ran,))
    return 'sh'


@pytest.fixture(scope='session')
def ldap2pg(request, sh_errout):
    return sh.Command(request.config.getoption("--ldap2pg")) \
             .bake(_log_msg=loggername_factory)


def pytest_addoption(parser):
    candidates = [
        "test/ldap2pg.sh",  # Wraps go run.
    ]
    if 'CI' in os.environ:
        # On CI, search for binary
        candidates[:0] = [
            "ldap2pg",  # in PATH
            "dist/ldap2pg_linux_amd64_v1/ldap2pg",  # in goreleaser dist dir.
        ]

    for candidate in candidates:
        try:
            default = sh.Command(candidate)._path
            break
        except sh.CommandNotFound:
            continue
    else:
        default = None

    if hasattr(default, 'decode'):
        default = default.decode('utf-8')

    parser.addoption(
        "--ldap2pg",
        default=default,
        help="Explicit path to ldap2pg binary.",
    )


def pytest_report_header(config):
    bin = config.getoption("--ldap2pg")
    return ["ldap2pg: %s" % bin]
