/*
 * snapd-xdg-open
 *
 * Copyright (C) 2016 Canonical Ltd.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>
 */

#include <gio/gio.h>
#include <stdlib.h>

static GDBusNodeInfo *introspection_data = NULL;

/* Introspection data for the service we are exporting */
static const gchar introspection_xml[] =
  "<node>"
  "  <interface name='com.canonical.SafeLauncher'>"
  "    <method name='OpenURL'>"
  "      <arg type='s' name='url' direction='in'/>"
  "    </method>"
  "  </interface>"
  "</node>";

static GMainLoop *loop;

#ifndef GLIB_VERSION_2_44
gboolean
g_strv_contains (const gchar * const *strv,
                 const gchar         *str)
{
  g_return_val_if_fail (strv != NULL, FALSE);
  g_return_val_if_fail (str != NULL, FALSE);

  for (; *strv != NULL; strv++)
    {
      if (g_str_equal (str, *strv))
        return TRUE;
    }

  return FALSE;
}
#endif

static void
handle_method_call (GDBusConnection       *connection,
                    const gchar           *sender,
                    const gchar           *object_path,
                    const gchar           *interface_name,
                    const gchar           *method_name,
                    GVariant              *parameters,
                    GDBusMethodInvocation *invocation,
                    gpointer               user_data)
{
  GError *error = NULL;

  const gchar * const whitelist[] = {
    "http",
    "https",
    "mailto",
    NULL
  };

  if (g_strcmp0 (method_name, "OpenURL") == 0)
    {
      const gchar *url;
      gchar *scheme;

      g_variant_get (parameters, "(&s)", &url);
      scheme = g_uri_parse_scheme (url);

      if (scheme == NULL)
        {
          g_dbus_method_invocation_return_error (invocation,
                                                 G_DBUS_ERROR,
                                                 G_DBUS_ERROR_INVALID_ARGS,
                                                 "unknown scheme: %s", url);
        }
      else if (g_strv_contains (whitelist, scheme))
        {
          if (g_app_info_launch_default_for_uri (url, NULL, &error))
            g_dbus_method_invocation_return_value (invocation, NULL);
          else
            {
              g_dbus_method_invocation_return_gerror (invocation, error);
              g_clear_error (&error);
            }
        }
      else
        {
          g_dbus_method_invocation_return_error (invocation,
                                                 G_DBUS_ERROR,
                                                 G_DBUS_ERROR_INVALID_ARGS,
                                                 "cannot open scheme: %s", scheme);
        }

      g_free (scheme);
    }
}

static const GDBusInterfaceVTable interface_vtable = { handle_method_call };

static void
on_bus_acquired (GDBusConnection *connection,
                 const gchar     *name,
                 gpointer         user_data)
{
  guint registration_id;

  registration_id = g_dbus_connection_register_object (connection,
                                                       "/",
                                                       introspection_data->interfaces[0],
                                                       &interface_vtable,
                                                       NULL,
                                                       NULL,
                                                       NULL);
  g_assert (registration_id > 0);
}

static void
on_name_lost (GDBusConnection *connection,
              const gchar     *name,
              gpointer         user_data)
{
  g_main_loop_quit (loop);
}

int
main (int argc, char *argv[])
{
  guint owner_id;

  introspection_data = g_dbus_node_info_new_for_xml (introspection_xml, NULL);
  g_assert (introspection_data != NULL);

  owner_id = g_bus_own_name (G_BUS_TYPE_SESSION,
                             "com.canonical.SafeLauncher",
                             G_BUS_NAME_OWNER_FLAGS_NONE,
                             on_bus_acquired,
                             NULL,
                             on_name_lost,
                             NULL,
                             NULL);

  loop = g_main_loop_new (NULL, FALSE);
  g_main_loop_run (loop);

  g_bus_unown_name (owner_id);

  g_dbus_node_info_unref (introspection_data);

  return 0;
}
