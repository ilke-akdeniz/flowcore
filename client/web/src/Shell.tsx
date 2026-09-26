import type { ReactNode } from "react";
import { NavLink, useLocation } from "react-router-dom";
import {
  AppShell,
  Badge,
  Burger,
  Button,
  Group,
  Menu,
  Text,
  Title,
} from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { api, type Session } from "./api";

const nav = [
  { to: "/", label: "My work", ready: true },
  { to: "/cases", label: "Cases", ready: false },
  { to: "/workflows", label: "Workflows", ready: false },
];

export function Shell({
  session,
  onSignedOut,
  children,
}: {
  session: Session;
  onSignedOut: () => void;
  children: ReactNode;
}) {
  const [opened, { toggle }] = useDisclosure();
  const location = useLocation();
  const person = session.signedInAs!;

  return (
    <AppShell
      header={{ height: 56 }}
      navbar={{
        width: 200,
        breakpoint: "sm",
        collapsed: { mobile: !opened },
      }}
      padding="lg"
    >
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Group gap="sm">
            <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" />
            <Title order={5}>Claims console</Title>
          </Group>

          <Group gap="sm">
            <Badge variant="light" size="sm">
              agents: {session.agentMode}
            </Badge>
            {/* Switching identity lives in the account menu, where a real
                application puts it — not beside the page content. */}
            <Menu position="bottom-end">
              <Menu.Target>
                <Button variant="subtle" size="compact-sm">
                  {person.name}
                </Button>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Label>Signed in as {person.title}</Menu.Label>
                <Menu.Divider />
                <Menu.Label>Switch to</Menu.Label>
                {session.roster
                  .filter((member) => member.reference !== person.reference)
                  .map((member) => (
                    <Menu.Item
                      key={member.reference}
                      onClick={async () => {
                        await api.signIn(member.reference);
                        onSignedOut();
                      }}
                    >
                      {member.name}
                      <Text span c="dimmed" size="xs">
                        {" "}
                        · {member.title}
                      </Text>
                    </Menu.Item>
                  ))}
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar p="sm">
        {nav.map((item) =>
          item.ready ? (
            <Button
              key={item.to}
              component={NavLink}
              to={item.to}
              variant={location.pathname === item.to ? "light" : "subtle"}
              justify="flex-start"
              fullWidth
              mb={4}
            >
              {item.label}
            </Button>
          ) : (
            <Button
              key={item.to}
              variant="subtle"
              justify="flex-start"
              fullWidth
              mb={4}
              disabled
            >
              {item.label}
            </Button>
          ),
        )}
      </AppShell.Navbar>

      <AppShell.Main>{children}</AppShell.Main>
    </AppShell>
  );
}
