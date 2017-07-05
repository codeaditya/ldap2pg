import os

import pytest
import sh


class PSQL(object):
    # A helper object to do SQL queries with real psql.
    def __init__(self):
        from sh import psql
        self.psql = psql

    def __call__(self, *a, **kw):
        return self.psql(*a, **kw)

    def select1(self, select):
        # Execute a SELECT and yield each line as a single value.
        return filter(None, (
            l.strip()
            for l in self('-tc', select, _iter=True)
        ))

    def members(self, role):
        # List members of role
        return self.select1(
            # Good old SQL injection. Who cares?
            "SELECT m.rolname FROM pg_roles AS m "
            "JOIN pg_auth_members a ON a.member = m.oid "
            "JOIN pg_roles AS r ON r.oid = a.roleid "
            " WHERE r.rolname = '%s' "
            "ORDER BY 1;" % (role,)
        )

    def roles(self):
        # List **all** roles
        return self.select1("SELECT rolname FROM pg_roles;")

    def superusers(self):
        # List superusers
        return self.select1(
            "SELECT rolname FROM pg_roles WHERE rolsuper IS TRUE;"
        )


@pytest.fixture(scope='session')
def psql():
    # Supply the PSQL helper as a pytest fixture.
    return PSQL()


class LDAP(object):
    # Helper to query LDAP with creds from envvars.
    def __init__(self):
        self.common_args = (
            '-v',
            '-h', os.environ['LDAP_HOST'],
            '-D', os.environ['LDAP_BIND'],
            '-w', os.environ['LDAP_PASSWORD'],
        )

        self.add = sh.ldapadd.bake(*self.common_args)
        self.search = sh.ldapsearch.bake(*self.common_args)
        self.delete = sh.ldapdelete.bake(*self.common_args)

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


@pytest.fixture(scope='module')
def dev(ldap, psql):
    # Load dev fixtures, to test them too !
    with open('dev-fixture.sql') as fo:
        psql(_in=fo)
    ldap.add('-f', 'dev-fixture.ldif')


@pytest.fixture(scope='module', autouse=True)
def flushall(ldap, psql):
    # Flush PostgreSQL and OpenLDAP from any data.

    psql('-tc', "DELETE FROM pg_catalog.pg_auth_members;")
    psql(
        '-tc',
        "DELETE FROM pg_catalog.pg_authid "
        "WHERE rolname != 'postgres' AND rolname NOT LIKE 'pg_%';",
    )

    for dn in reversed(list(ldap.search_sub_dn(base='dc=ldap2pg,dc=local'))):
        if 'cn=admin,' not in dn:
            ldap.delete(dn)