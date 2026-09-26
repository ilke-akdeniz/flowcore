import {
  Anchor,
  Button,
  Card,
  Center,
  Group,
  Stack,
  Text,
  Title,
} from "@mantine/core";
import { api, type Session } from "../api";

// Sign-in is a choice from a list, not authentication.
//
// It exists because the shape is what makes an application read as software —
// every internal console starts here — and because it gives the console one
// place to say what it is, reaching every visitor without putting a word of
// explanation on a working screen.
export function SignIn({
  session,
  onSignedIn,
}: {
  session: Session;
  onSignedIn: () => void;
}) {
  const signIn = async (reference: string) => {
    await api.signIn(reference);
    onSignedIn();
  };

  return (
    <Center mih="100vh" p="md">
      <Stack maw={520} w="100%" gap="lg">
        <Stack gap={4}>
          <Title order={2}>Claims console</Title>
          <Text c="dimmed" size="sm">
            An insurer's internal console for assessing claims and underwriting new
            policies, built on{" "}
            <Anchor
              href="https://github.com/mike-akdeniz/flowcore"
              target="_blank"
              rel="noreferrer"
            >
              FlowCore
            </Anchor>
            . Some steps are decided by people and some by an AI agent.
          </Text>
          <Text c="dimmed" size="sm">
            These are demo accounts — pick one. There are no passwords, and your
            work is yours alone: every visitor gets their own copy of the data.
          </Text>
        </Stack>

        <Card withBorder padding={0}>
          <Stack gap={0}>
            {session.roster.map((member, index) => (
              <Group
                key={member.reference}
                justify="space-between"
                p="md"
                style={{
                  borderTop:
                    index === 0
                      ? undefined
                      : "1px solid var(--mantine-color-default-border)",
                }}
              >
                <Stack gap={0}>
                  <Text fw={500}>{member.name}</Text>
                  <Text size="sm" c="dimmed">
                    {member.title}
                  </Text>
                </Stack>
                <Button
                  variant="light"
                  onClick={() => void signIn(member.reference)}
                >
                  Sign in
                </Button>
              </Group>
            ))}
          </Stack>
        </Card>

        <Text size="xs" c="dimmed">
          Agent steps: {session.agentMode}.
        </Text>
      </Stack>
    </Center>
  );
}
