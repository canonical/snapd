/*
 * Copyright © 2012 Intel Corp
 *
 * This library is free software; you can redistribute it and/or
 * modify it under the terms of the GNU Lesser General Public
 * License as published by the Free Software Foundation; either
 * version 2 of the License, or (at your option) any later version.
 *
 * This library is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
 * Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General
 * Public License along with this library; if not, see <http://www.gnu.org/licenses/>.
 */

#define GOA_API_IS_SUBJECT_TO_CHANGE
#include <goa/goa.h>

#include <locale.h>

int
main (int argc, char **argv)
{
  GError *error = NULL;
  GoaClient *client;
  GList *accounts, *l;
  GoaAccount *account;

  setlocale (LC_ALL, "");

  client = goa_client_new_sync (NULL, &error);
  if (!client) {
    g_printerr ("Could not create GoaClient: %s", error->message);
    return 1;
  }

  accounts = goa_client_get_accounts (client);
  for (l = accounts; l != NULL; l = l->next) {
    GoaOAuth2Based *oauth2 = NULL;

    account = goa_object_get_account (GOA_OBJECT (l->data));
    g_print ("%s at %s (%s)\n",
             goa_account_get_presentation_identity (account),
             goa_account_get_provider_name (account),
             goa_account_get_provider_type (account));
    oauth2 = goa_object_get_oauth2_based (GOA_OBJECT (l->data));
    if (oauth2) {
      gchar *access_token;
      if (goa_oauth2_based_call_get_access_token_sync (oauth2,
                                                       &access_token,
                                                       NULL,
                                                       NULL,
                                                       NULL)) {
        g_print ("\tAccessToken: %s\n", access_token);
        g_free (access_token);
      }
      g_print ("\tClientId: %s\n\tClientSecret: %s\n",
               goa_oauth2_based_get_client_id (oauth2),
               goa_oauth2_based_get_client_secret (oauth2));
    }
    g_clear_object (&oauth2);
  }

  g_list_free_full (accounts, (GDestroyNotify) g_object_unref);

  return 0;
}
